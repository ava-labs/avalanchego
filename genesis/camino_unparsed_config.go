// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
)

type UnparsedCamino struct {
	VerifyNodeSignature bool `json:"verifyNodeSignature"`
	LockModeBondDeposit bool `json:"lockModeBondDeposit"`
}

func (us UnparsedCamino) Parse() (genesis.Camino, error) {
	c := genesis.Camino{
		VerifyNodeSignature: us.VerifyNodeSignature,
		LockModeBondDeposit: us.LockModeBondDeposit,
	}
	return c, nil
}

func (us *UnparsedCamino) Unparse(p genesis.Camino, networkID uint32) error {
	us.VerifyNodeSignature = p.VerifyNodeSignature
	us.LockModeBondDeposit = p.LockModeBondDeposit

	return nil
}
