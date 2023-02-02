package types

import (
	"github.com/ArkeoNetwork/arkeo/common"

	. "gopkg.in/check.v1"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

type MsgOpenContractSuite struct{}

var _ = Suite(&MsgOpenContractSuite{})

func (MsgOpenContractSuite) TestValidateBasic(c *C) {
	// setup
	pubkey := GetRandomPubKey()
	acct, err := pubkey.GetMyAddress()
	c.Assert(err, IsNil)

	// invalid address
	msg := MsgOpenContract{
		Creator: "invalid address",
	}
	err = msg.ValidateBasic()
	c.Check(err, ErrIs, sdkerrors.ErrInvalidAddress)

	msg = MsgOpenContract{
		Creator: acct.String(),
		PubKey:  pubkey,
		Client:  pubkey,
		Chain:   common.BTCChain.String(),
	}
	err = msg.ValidateBasic()
	c.Check(err, ErrIs, ErrOpenContractDuration)

	msg.Duration = 100
	err = msg.ValidateBasic()
	c.Check(err, ErrIs, ErrOpenContractRate)

	msg.Rate = 100
	err = msg.ValidateBasic()
	c.Assert(err, IsNil)
}
