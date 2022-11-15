// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

type UnparsedCamino struct {
	VerifyNodeSignature bool   `json:"verifyNodeSignature"`
	LockModeBondDeposit bool   `json:"lockModeBondDeposit"`
	InitialAdmin        string `json:"initialAdmin"`
}

func (us UnparsedCamino) Parse() (genesis.Camino, error) {
	c := genesis.Camino{
		VerifyNodeSignature: us.VerifyNodeSignature,
		LockModeBondDeposit: us.LockModeBondDeposit,
	}

	_, _, avaxAddrBytes, err := address.Parse(us.InitialAdmin)
	if err != nil {
		return c, err
	}
	avaxAddr, err := ids.ToShortID(avaxAddrBytes)
	if err != nil {
		return c, err
	}
	c.InitialAdmin = avaxAddr
	return c, nil
}

func (us *UnparsedCamino) Unparse(p genesis.Camino, networkID uint32) error {
	us.VerifyNodeSignature = p.VerifyNodeSignature
	us.LockModeBondDeposit = p.LockModeBondDeposit

	avaxAddr, err := address.Format(
		"X",
		constants.GetHRP(networkID),
		p.InitialAdmin.Bytes(),
	)
	if err != nil {
		return err
	}
	us.InitialAdmin = avaxAddr

	return nil
}
