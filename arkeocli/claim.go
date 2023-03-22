package arkeocli

import (
	"encoding/hex"
	"fmt"
	"strconv"

	"cosmossdk.io/errors"
	"github.com/arkeonetwork/arkeo/common"
	"github.com/arkeonetwork/arkeo/x/arkeo/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/spf13/cobra"
)

func newClaimCmd() *cobra.Command {
	claimCmd := &cobra.Command{
		Use:   "claim",
		Short: "claim accrued contract income",
		Args:  cobra.ExactArgs(0),
		RunE:  runClaimCmd,
	}

	flags.AddTxFlagsToCmd(claimCmd)
	claimCmd.Flags().String("provider-pubkey", "", "provider pubkey")
	claimCmd.Flags().String("contract-id", "", "id of contract")
	claimCmd.Flags().String("service", "", "service name")
	claimCmd.Flags().Int64("nonce", 0, "requests claimed (must increment each call)")
	return claimCmd
}

func runClaimCmd(cmd *cobra.Command, args []string) (err error) {
	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return err
	}
	nonce, _ := cmd.Flags().GetInt64("nonce")
	if nonce == 0 {
		nonceString, err := promptForArg(cmd, "Specify nonce: ")
		if err != nil {
			return err
		}
		nonce, err = strconv.ParseInt(nonceString, 10, 64)
		if err != nil {
			return err
		}
	}
	key, err := ensureKeys(cmd)
	if err != nil {
		return err
	}
	addr, err := key.GetAddress()
	if err != nil {
		return
	}

	clientCtx = clientCtx.WithFromName(key.Name).WithFromAddress(addr)
	if err = client.SetCmdClientContext(cmd, clientCtx); err != nil {
		return
	}

	creator := addr.String()
	spenderPubkeyStr, err := toPubkey(cmd, addr)
	if err != nil {
		return
	}

	spk, err := common.NewPubKey(spenderPubkeyStr)
	if err != nil {
		return err
	}

	argProviderPubkey, _ := cmd.Flags().GetString("provider-pubkey")
	if argProviderPubkey == "" {
		argProviderPubkey, err = promptForArg(cmd, "Specify provider pubkey: ")
		if err != nil {
			return
		}
	}

	providerPubKey, err := common.NewPubKey(argProviderPubkey)
	if err != nil {
		return err
	}
	_ = providerPubKey

	contractID, _ := cmd.Flags().GetUint64("contract-id")
	if contractID == 0 {
		argService, _ := cmd.Flags().GetString("service")
		if argService == "" {
			argService, err = promptForArg(cmd, "Specify service (e.g. gaia-mainnet-rpc-archive, btc-mainnet-fullnode, etc): ")
			if err != nil {
				return err
			}
		}
		_ = argService

		clientCtx, err := client.GetClientQueryContext(cmd)
		if err != nil {
			return err
		}

		queryClient := types.NewQueryClient(clientCtx)

		params := &types.QueryActiveContractRequest{
			Spender:  spenderPubkeyStr,
			Provider: argProviderPubkey,
			Service:  argService,
		}

		res, err := queryClient.ActiveContract(cmd.Context(), params)
		if err != nil {
			// not found "rpc error: code = NotFound desc = not found: key not found"
			return errors.Wrapf(err, "could not find active contract for %s:%s:%s", spenderPubkeyStr, argProviderPubkey, argService)
		}

		contractID = res.GetContract().Id
		cmd.Println("res")
	}

	// "$CONTRACT_ID:$CLIENT_PUBKEY:$NONCE"
	signStr := fmt.Sprintf("%d:%s:%d", contractID, spenderPubkeyStr, nonce)
	signBytes := []byte(signStr)
	signature, pubkey, err := clientCtx.Keyring.Sign(key.Name, signBytes)
	if err != nil {
		return errors.Wrapf(err, "error signing")
	}

	// verify signature
	if !pubkey.VerifySignature(signBytes, signature) {
		return fmt.Errorf("signature verification failed")
	}

	sigHex := hex.EncodeToString(signature)
	_ = sigHex

	msg := types.NewMsgClaimContractIncome(
		creator,
		contractID,
		spk,
		nonce,
		signature,
	)
	if err := msg.ValidateBasic(); err != nil {
		return err
	}

	return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), msg)
}
