package types

import sdk "github.com/cosmos/cosmos-sdk/types"

func IsValidAddress(address string, chain Chain) bool {
	switch chain {
	case ETHEREUM:
		return IsValidEthAddress(address)
	case ARKEO:
		_, err := sdk.AccAddressFromBech32(address)
		return err == nil
	case THORCHAIN:
		return false // thorchain not supported yet
	default:
		return false
	}
}
