package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"text/template"
	"time"

	arkeo "github.com/arkeonetwork/arkeo/x/arkeo/types"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog/log"
	"gopkg.in/yaml.v3"
)

////////////////////////////////////////////////////////////////////////////////////////
// Operation
////////////////////////////////////////////////////////////////////////////////////////

type Operation interface {
	Execute(arkeo *os.Process, logs chan string) error
	OpType() string
}

type OpBase struct {
	Type string `json:"type"`
}

func (op *OpBase) OpType() string {
	return op.Type
}

func NewOperation(opMap map[string]any) Operation {
	// ensure type is provided
	t, ok := opMap["type"].(string)
	if !ok {
		log.Fatal().Interface("type", opMap["type"]).Msg("operation type is not a string")
	}

	// create the operation for the type
	var op Operation
	switch t {
	case "state":
		op = &OpState{}
	case "check":
		op = &OpCheck{}
	case "create-blocks":
		op = &OpCreateBlocks{}
	case "tx-send":
		op = &OpTxSend{}
	case "tx-bond-provider":
		op = &OpTxBondProvider{}
	case "tx-mod-provider":
		op = &OpTxModProvider{}
	case "tx-open-contract":
		op = &OpTxOpenContract{}
	case "tx-close-contract":
		op = &OpTxCloseContract{}
	default:
		log.Fatal().Str("type", t).Msg("unknown operation type")
	}

	// create decoder supporting embedded structs and weakly typed input
	dec, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		WeaklyTypedInput: true,
		ErrorUnused:      true,
		Squash:           true,
		Result:           op,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create decoder")
	}

	switch op.(type) {
	// internal types have MarshalJSON methods necessary to decode
	case *OpTxSend, *OpTxBondProvider, *OpTxModProvider, *OpTxOpenContract, *OpTxCloseContract:
		// encode as json
		buf := bytes.NewBuffer(nil)
		enc := json.NewEncoder(buf)
		err = enc.Encode(opMap)
		if err != nil {
			log.Fatal().Interface("op", opMap).Err(err).Msg("failed to encode operation")
		}

		// unmarshal json to op
		err = json.NewDecoder(buf).Decode(op)

	default:
		err = dec.Decode(opMap)
	}
	if err != nil {
		log.Fatal().Interface("op", opMap).Err(err).Msg("failed to decode operation")
	}

	// require check description and default status check to 200 if endpoint is set
	if oc, ok := op.(*OpCheck); ok && oc.Endpoint != "" {
		if oc.Description == "" {
			log.Fatal().Interface("op", opMap).Msg("check operation must have a description")
		}
		if oc.Status == 0 {
			oc.Status = 200
		}
	}

	return op
}

////////////////////////////////////////////////////////////////////////////////////////
// OpState
////////////////////////////////////////////////////////////////////////////////////////

type OpState struct {
	OpBase  `yaml:",inline"`
	Genesis map[string]any `json:"genesis"`
}

func (op *OpState) Execute(*os.Process, chan string) error {
	// load genesis file
	f, err := os.OpenFile(os.ExpandEnv("/regtest/.arkeo/config/genesis.json"), os.O_RDWR, 0o644)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open genesis file")
	}

	// unmarshal genesis into map
	var genesisMap map[string]any
	err = json.NewDecoder(f).Decode(&genesisMap)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to decode genesis file")
	}

	// merge updates into genesis
	genesis := deepMerge(genesisMap, op.Genesis)

	// reset file
	err = f.Truncate(0)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to truncate genesis file")
	}
	_, err = f.Seek(0, 0)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to seek genesis file")
	}

	// marshal genesis into file
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	err = enc.Encode(genesis)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to encode genesis file")
	}

	return f.Close()
}

////////////////////////////////////////////////////////////////////////////////////////
// OpCheck
////////////////////////////////////////////////////////////////////////////////////////

type OpCheck struct {
	OpBase      `yaml:",inline"`
	Description string            `json:"description"`
	Endpoint    string            `json:"endpoint"`
	Params      map[string]string `json:"params"`
	Status      int               `json:"status"`
	Asserts     []string          `json:"asserts"`
}

func (op *OpCheck) Execute(_ *os.Process, logs chan string) error {
	// abort if no endpoint is set (empty check op is allowed for breakpoint convenience)
	if op.Endpoint == "" {
		return fmt.Errorf("check")
	}

	// build request
	req, err := http.NewRequest("GET", op.Endpoint, nil)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to build request")
	}

	// add params
	q := req.URL.Query()
	for k, v := range op.Params {
		q.Add(k, v)
	}
	req.URL.RawQuery = q.Encode()

	// send request
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Err(err).Msg("failed to send request")
		return err
	}

	// read response
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Err(err).Msg("failed to read response")
		return err
	}

	// ensure status code matches
	if resp.StatusCode != op.Status {
		// dump pretty output for debugging
		fmt.Println(ColorPurple + "\nOperation:" + ColorReset)
		_ = yaml.NewEncoder(os.Stdout).Encode(op)
		fmt.Println(ColorPurple + "\nEndpoint Response:" + ColorReset)
		fmt.Println(string(buf) + "\n")

		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// pipe response to jq for assertions
	for _, a := range op.Asserts {
		// render the assert expression (used for native_txid)
		tmpl := template.Must(template.Must(templates.Clone()).Parse(a))
		expr := bytes.NewBuffer(nil)
		err = tmpl.Execute(expr, nil)
		if err != nil {
			log.Fatal().Err(err).Msg("failed to render assert expression")
		}
		a = expr.String()

		cmd := exec.Command("jq", "-e", a)
		cmd.Stdin = bytes.NewReader(buf)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if cmd.ProcessState.ExitCode() == 1 {
				// dump process logs if the assert expression failed
				fmt.Println(ColorPurple + "\nLogs:" + ColorReset)
				dumpLogs(logs)
			}

			// dump pretty output for debugging
			fmt.Println(ColorPurple + "\nOperation:" + ColorReset)
			_ = yaml.NewEncoder(os.Stdout).Encode(op)
			fmt.Println(ColorPurple + "\nFailed Assert: " + ColorReset + expr.String())
			fmt.Println(ColorPurple + "\nEndpoint Response:" + ColorReset)
			fmt.Println(string(buf) + "\n")

			// log fatal on syntax errors and skip logs
			if cmd.ProcessState.ExitCode() != 1 {
				drainLogs(logs)
				fmt.Println(ColorRed + string(out) + ColorReset)
			}

			return err
		}
	}

	return nil
}

////////////////////////////////////////////////////////////////////////////////////////
// OpCreateBlocks
////////////////////////////////////////////////////////////////////////////////////////

type OpCreateBlocks struct {
	OpBase `yaml:",inline"`
	Count  int  `json:"count"`
	Exit   *int `json:"exit"`
}

func (op *OpCreateBlocks) Execute(p *os.Process, logs chan string) error {
	// clear existing log output
	drainLogs(logs)

	for i := 0; i < op.Count; i++ {
		// http request to localhost to unblock block creation
		_, err := httpClient.Get("http://localhost:8080/newBlock")
		if err != nil {
			// if exit code is not set this was unexpected
			if op.Exit == nil {
				log.Err(err).Msg("failed to create block")
				return err
			}

			// if exit code is set, this was expected
			if processRunning(p.Pid) {
				log.Err(err).Msg("block did not exit as expected")
				return err
			}

			// if process is not running, check exit code
			ps, err := p.Wait()
			if err != nil {
				log.Err(err).Msg("failed to wait for process")
				return err
			}
			if ps.ExitCode() != *op.Exit {
				log.Error().Int("exit", ps.ExitCode()).Int("expect", *op.Exit).Msg("bad exit code")
				return err
			}

			// exit code is correct, return nil
			return nil
		}
	}

	// if exit code is set, this was unexpected
	if op.Exit != nil {
		log.Error().Int("expect", *op.Exit).Msg("expected exit code")
		return errors.New("expected exit code")
	}

	// avoid minor raciness after end block
	time.Sleep(200 * time.Millisecond * getTimeFactor())

	return nil
}

// ------------------------------ OpTxSend ------------------------------

type OpTxSend struct {
	OpBase       `yaml:",inline"`
	bank.MsgSend `yaml:",inline"`
	Sequence     *int64 `json:"sequence"`
}

func (op *OpTxSend) Execute(_ *os.Process, logs chan string) error {
	signer := sdk.MustAccAddressFromBech32(op.FromAddress)
	return sendMsg(&op.MsgSend, signer, op.Sequence, op, logs)
}

// ------------------------------ OpTxBondProvider ------------------------------

type OpTxBondProvider struct {
	OpBase                `yaml:",inline"`
	arkeo.MsgBondProvider `yaml:",inline"`
	Signer                string `json:"signer"`
	Sequence              *int64 `json:"sequence"`
}

func (op *OpTxBondProvider) Execute(_ *os.Process, logs chan string) error {
	signer := sdk.MustAccAddressFromBech32(op.Signer)
	return sendMsg(&op.MsgBondProvider, signer, op.Sequence, op, logs)
}

// ------------------------------ OpTxModProvider ------------------------------

type OpTxModProvider struct {
	OpBase               `yaml:",inline"`
	arkeo.MsgModProvider `yaml:",inline"`
	Signer               string `json:"signer"`
	Sequence             *int64 `json:"sequence"`
}

func (op *OpTxModProvider) Execute(_ *os.Process, logs chan string) error {
	signer := sdk.MustAccAddressFromBech32(op.Signer)
	return sendMsg(&op.MsgModProvider, signer, op.Sequence, op, logs)
}

// ------------------------------ OpTxOpenContract ------------------------------

type OpTxOpenContract struct {
	OpBase                `yaml:",inline"`
	arkeo.MsgOpenContract `yaml:",inline"`
	Signer                string `json:"signer"`
	Sequence              *int64 `json:"sequence"`
}

func (op *OpTxOpenContract) Execute(_ *os.Process, logs chan string) error {
	signer := sdk.MustAccAddressFromBech32(op.Signer)
	return sendMsg(&op.MsgOpenContract, signer, op.Sequence, op, logs)
}

// ------------------------------ OpTxCloseContract ------------------------------

type OpTxCloseContract struct {
	OpBase                 `yaml:",inline"`
	arkeo.MsgCloseContract `yaml:",inline"`
	Signer                 string `json:"signer"`
	Sequence               *int64 `json:"sequence"`
}

func (op *OpTxCloseContract) Execute(_ *os.Process, logs chan string) error {
	signer := sdk.MustAccAddressFromBech32(op.Signer)
	return sendMsg(&op.MsgCloseContract, signer, op.Sequence, op, logs)
}

////////////////////////////////////////////////////////////////////////////////////////
// Helpers
////////////////////////////////////////////////////////////////////////////////////////

func sendMsg(msg sdk.Msg, signer sdk.AccAddress, seq *int64, op any, logs chan string) error {
	// check that message is valid
	err := msg.ValidateBasic()
	if err != nil {
		enc := json.NewEncoder(os.Stdout) // json instead of yaml to encode amount
		enc.SetIndent("", "  ")
		_ = enc.Encode(op)
		log.Fatal().Err(err).Msg("failed to validate basic")
	}

	// custom client context
	buf := bytes.NewBuffer(nil)
	ctx := clientCtx.WithFromAddress(signer)
	ctx = ctx.WithFromName(addressToName[signer.String()])
	ctx = ctx.WithOutput(buf)

	// override the sequence if provided
	txf := txFactory
	if seq != nil {
		txf = txFactory.WithSequence(uint64(*seq))
	}

	// send message
	err = tx.GenerateOrBroadcastTxWithFactory(ctx, txf, msg)
	if err != nil {
		fmt.Println(ColorPurple + "\nOperation:" + ColorReset)
		enc := json.NewEncoder(os.Stdout) // json instead of yaml to encode amount
		enc.SetIndent("", "  ")
		_ = enc.Encode(op)
		fmt.Println(ColorPurple + "\nTx Output:" + ColorReset)
		drainLogs(logs)
		return err
	}

	// extract txhash from output json
	var txRes sdk.TxResponse
	err = encodingConfig.Marshaler.UnmarshalJSON(buf.Bytes(), &txRes)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to unmarshal tx response")
	}

	// fail if tx did not send, otherwise add to out native tx ids
	if txRes.Code != 0 {
		log.Debug().Uint32("code", txRes.Code).Str("log", txRes.RawLog).Msg("tx send failed")
	} else {
		nativeTxIDs = append(nativeTxIDs, txRes.TxHash)
	}

	return err
}
