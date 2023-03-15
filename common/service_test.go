package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestString(t *testing.T) {
	var service Service = 0
	require.Equal(t, "unknown", service.String())
	service = StarWarsService
	require.Equal(t, "swapi.dev", service.String())
	service = BaseService
	require.Equal(t, "github.com/arkeonetwork/arkeo-mainnet-fullnode", service.String())
	service = BTCService
	require.Equal(t, "btc-mainnet-fullnode", service.String())
	service = ETHService
	require.Equal(t, "eth-mainnet-fullnode", service.String())
	service = GAIAChainRPCArchiveService
	require.Equal(t, "gaia-mainnet-rpc-archive", service.String())
	service = 6
	require.Equal(t, "unknown", service.String())
}
