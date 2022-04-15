// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package propertyfx

import (
	"errors"

	"github.com/chain4travel/caminogo/snow"
	"github.com/chain4travel/caminogo/vms/components/verify"
	"github.com/chain4travel/caminogo/vms/secp256k1fx"
)

var errNilMintOperation = errors.New("nil mint operation")

type MintOperation struct {
	MintInput   secp256k1fx.Input `serialize:"true" json:"mintInput"`
	MintOutput  MintOutput        `serialize:"true" json:"mintOutput"`
	OwnedOutput OwnedOutput       `serialize:"true" json:"ownedOutput"`
}

func (op *MintOperation) InitCtx(ctx *snow.Context) {
	op.MintOutput.OutputOwners.InitCtx(ctx)
	op.OwnedOutput.OutputOwners.InitCtx(ctx)
}

func (op *MintOperation) Cost() (uint64, error) {
	return op.MintInput.Cost()
}

func (op *MintOperation) Outs() []verify.State {
	return []verify.State{
		&op.MintOutput,
		&op.OwnedOutput,
	}
}

func (op *MintOperation) Verify() error {
	switch {
	case op == nil:
		return errNilMintOperation
	default:
		return verify.All(&op.MintInput, &op.MintOutput, &op.OwnedOutput)
	}
}
