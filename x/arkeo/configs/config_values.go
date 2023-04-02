package configs

import (
	"fmt"

	"github.com/blang/semver"
)

// ConfigName the name we used to get constant values
type ConfigName int

const (
	HandlerBondProvider ConfigName = iota
	HandlerModProvider
	HandlerOpenContract
	HandlerCloseContract
	HandlerClaimContractIncome
	MaxContractLength
	OpenContractCost
	MinProviderBond
	ReserveTax
	AffilateFee
	BlocksPerYear
	EmissionCurve
	ValidatorPayoutCycle
)

var nameToString = map[ConfigName]string{
	HandlerBondProvider:        "HandlerBondProvider",
	HandlerModProvider:         "HandlerModProvider",
	HandlerOpenContract:        "HandlerOpenContract",
	HandlerCloseContract:       "HandlerCloseContract",
	HandlerClaimContractIncome: "HandlerClaimContractIncome",
	MaxContractLength:          "MaxContractLength",
	OpenContractCost:           "OpenContractCost",
	MinProviderBond:            "MinProviderBond",
	ReserveTax:                 "ReserveTax",
	AffilateFee:                "AffilateFee",
	BlocksPerYear:              "BlocksPerYear",
	EmissionCurve:              "EmissionCurve",
	ValidatorPayoutCycle:       "ValidatorPayoutCycle",
}

// String implement fmt.stringer
func (cn ConfigName) String() string {
	val, ok := nameToString[cn]
	if !ok {
		return "NA"
	}
	return val
}

// ConfigValues define methods used to get constant values
type ConfigValues interface {
	fmt.Stringer
	GetInt64Value(name ConfigName) int64
	GetBoolValue(name ConfigName) bool
	GetStringValue(name ConfigName) string
}

// GetConfigValues will return an  implementation of ConfigValues which provide ways to get constant values
func GetConfigValues(ver semver.Version) ConfigValues {
	if ver.GTE(semver.MustParse("0.0.0")) {
		return NewConfigValue010()
	}
	return nil
}
