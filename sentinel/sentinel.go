package sentinel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/tendermint/tendermint/libs/log"

	"github.com/arkeonetwork/arkeo/common"
	"github.com/arkeonetwork/arkeo/sentinel/conf"

	"github.com/gorilla/handlers"
)

type Proxy struct {
	Metadata   Metadata
	Config     conf.Configuration
	MemStore   *MemStore
	ClaimStore *ClaimStore
	logger     log.Logger
	proxies    map[string]*url.URL
}

func NewProxy(config conf.Configuration) Proxy {
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))
	claimStore, err := NewClaimStore(config.ClaimStoreLocation)
	if err != nil {
		panic(err)
	}

	return Proxy{
		Metadata:   NewMetadata(config),
		Config:     config,
		MemStore:   NewMemStore(config.SourceChain, logger),
		ClaimStore: claimStore,
		proxies:    loadProxies(),
		logger:     logger,
	}
}

func loadProxies() map[string]*url.URL {
	proxies := make(map[string]*url.URL)
	for serviceName := range common.ServiceLookup {
		// if we have an override for a given service, parse that instead of
		// the default below
		env, envOk := os.LookupEnv(strings.ToUpper(serviceName))
		if envOk {
			proxies[serviceName] = common.MustParseURL(env)
			continue
		}

		// parse default values for services
		switch serviceName {
		case "btc-mainnet-fullnode":
			proxies[serviceName] = common.MustParseURL("http://thorchain:password@bitcoin-daemon:8332")
		case "bch-mainnet-fullnode":
			proxies[serviceName] = common.MustParseURL("http://infra:password@bitcoin-cash-daemon:8332")
		case "doge-mainnet-fullnode":
			proxies[serviceName] = common.MustParseURL("http://thorchain:password@doge-daemon:8332")
		case "ltc-mainnet-fullnode":
			proxies[serviceName] = common.MustParseURL("http://infra:password@litecoin-daemon:8332")
		case "arkeo-mainnet-fullnode":
			proxies[serviceName] = common.MustParseURL("http://arkeo:1317")
		case "eth-mainnet-fullnode":
			proxies[serviceName] = common.MustParseURL("http://ethereum-daemon:8545")
		case "gaia-mainnet-grpc":
			proxies[serviceName] = common.MustParseURL("http://gaia-daemon:9090")
		case "gaia-mainnet-rpc":
			proxies[serviceName] = common.MustParseURL("http://gaia-daemon:26657")
		case "swapi.dev":
			proxies[serviceName] = common.MustParseURL(fmt.Sprintf("https://%s", serviceName))
		default:
			proxies[serviceName] = common.MustParseURL(fmt.Sprintf("http://%s", serviceName))
		}
	}
	return proxies
}

// Given a request send it to the appropriate url
func (p Proxy) handleRequestAndRedirect(w http.ResponseWriter, r *http.Request) {
	// remove arkauth query arg
	values := r.URL.Query()
	values.Del(QueryArkAuth)
	r.URL.RawQuery = values.Encode()

	parts := strings.Split(r.URL.Path, "/")
	serviceName := parts[1]

	uri, exists := p.proxies[serviceName]
	if !exists {
		respondWithError(w, "could not find service", http.StatusBadRequest)
		return
	}

	r.URL.Scheme = uri.Scheme
	r.URL.Host = uri.Host
	r.URL.User = uri.User
	parts[1] = uri.Path // replace service name with uri path (if exists)
	r.URL.Path = path.Join(parts...)

	// Sanitize URL
	// ensure path always has "/" prefix
	if len(r.URL.Path) > 1 && !strings.HasPrefix(r.URL.Path, "/") {
		r.URL.Path = fmt.Sprintf("/%s", r.URL.Path)
	}

	// Serve a reverse proxy for a given url
	// create the reverse proxy
	proxy := common.NewSingleHostReverseProxy(r.URL)

	// Note that ServeHttp is non blocking and uses a go routine under the hood
	proxy.ServeHTTP(w, r)
}

func (p Proxy) handleMetadata(w http.ResponseWriter, r *http.Request) {
	r.Header.Set("Content-Type", "application/json")

	d, _ := json.Marshal(p.Metadata)
	_, _ = w.Write(d)
}

func (p Proxy) handleOpenClaims(w http.ResponseWriter, r *http.Request) {
	r.Header.Set("Content-Type", "application/json")

	open_claims := make([]Claim, 0)
	for _, claim := range p.ClaimStore.List() {
		if claim.Claimed {
			p.logger.Info("already claimed")
			continue
		}
		contract, err := p.MemStore.Get(claim.Key())
		if err != nil {
			p.logger.Error("bad fetch")
			continue
		}

		if contract.IsExpired(p.MemStore.GetHeight()) {
			_ = p.ClaimStore.Remove(claim.Key()) // clear expired
			p.logger.Info("claim expired")
			continue
		}

		open_claims = append(open_claims, claim)

	}

	d, _ := json.Marshal(open_claims)
	_, _ = w.Write(d)
}

func (p Proxy) handleActiveContract(w http.ResponseWriter, r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	path := r.URL.Path

	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		respondWithError(w, "not enough parameters", http.StatusBadRequest)
		return
	}

	providerPK := p.Config.ProviderPubKey

	service, err := common.NewService(parts[2])
	if err != nil {
		p.logger.Error("fail to parse service", "error", err, "service", parts[2])
		respondWithError(w, fmt.Sprintf("bad provider pubkey: %s", err), http.StatusBadRequest)
		return
	}

	spenderPK, err := common.NewPubKey(parts[3])
	if err != nil {
		p.logger.Error("fail to parse spender pubkey", "error", err, "service", parts[3])
		respondWithError(w, "Invalid spender pubkey", http.StatusBadRequest)
		return
	}

	contract, err := p.MemStore.GetActiveContract(providerPK, service, spenderPK)
	if err != nil {
		p.logger.Error("fail to get contract from memstore", "error", err, "provider", providerPK, "service", service, "spender", spenderPK)
		respondWithError(w, fmt.Sprintf("fetch contract error: %s", err), http.StatusBadRequest)
		return
	}

	d, _ := json.Marshal(contract)
	_, _ = w.Write(d)
}

func (p Proxy) handleClaim(w http.ResponseWriter, r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	path := r.URL.Path

	parts := strings.Split(path, "/")
	if len(parts) < 3 {
		respondWithError(w, "not enough parameters", http.StatusBadRequest)
		return
	}

	contractId, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		p.logger.Error("fail to parse contractId", "error", err, "contractId", parts[2])
		respondWithError(w, fmt.Sprintf("bad contractId: %s", err), http.StatusBadRequest)
		return
	}

	claim := NewClaim(contractId, nil, 0, "")
	claim, err = p.ClaimStore.Get(claim.Key())
	if err != nil {
		p.logger.Error("fail to get claim from memstore", "error", err, "key", claim.Key())
		respondWithError(w, fmt.Sprintf("fetch contract error: %s", err), http.StatusBadRequest)
		return
	}

	d, _ := json.Marshal(claim)
	_, _ = w.Write(d)
}

func (p Proxy) Run() {
	p.logger.Info("Starting Sentinel (reverse proxy)....")
	p.Config.Print()

	go p.EventListener(p.Config.EventStreamHost)

	mux := http.NewServeMux()

	// start server
	mux.Handle(RoutesMetaData, handlers.LoggingHandler(os.Stdout, http.HandlerFunc(p.handleMetadata)))
	mux.Handle(RoutesActiveContract, handlers.LoggingHandler(os.Stdout, http.HandlerFunc(p.handleActiveContract)))
	mux.Handle(RoutesClaim, handlers.LoggingHandler(os.Stdout, http.HandlerFunc(p.handleClaim)))
	mux.Handle(RoutesOpenClaims, handlers.LoggingHandler(os.Stdout, http.HandlerFunc(p.handleOpenClaims)))
	mux.Handle(RoutesDefault, p.auth(
		handlers.LoggingHandler(
			os.Stdout,
			handlers.ProxyHeaders(
				http.HandlerFunc(p.handleRequestAndRedirect),
			),
		),
	))

	if err := http.ListenAndServe(fmt.Sprintf(":%s", p.Config.Port), mux); err != nil {
		panic(err)
	}
}

func respondWithError(w http.ResponseWriter, message string, code int) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, err := json.Marshal(payload)
	if err != nil {
		response = []byte(`{"error": "failed to marshal response payload"}`)
		code = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write(response)
}
