// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1fx

import (
	"errors"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var errNilMintOperation = errors.New("nil mint operation")

type MintOperation struct {
	MintInput      Input          `serialize:"true" json:"mintInput"`
	MintOutput     MintOutput     `serialize:"true" json:"mintOutput"`
	TransferOutput TransferOutput `serialize:"true" json:"transferOutput"`
}

func (op *MintOperation) InitCtx(ctx *snow.Context) {
	op.MintOutput.OutputOwners.InitCtx(ctx)
	op.TransferOutput.OutputOwners.InitCtx(ctx)
}

func (op *MintOperation) Cost() (uint64, error) {
	return op.MintInput.Cost()
}

func (op *MintOperation) Outs() []verify.State {
	return []verify.State{&op.MintOutput, &op.TransferOutput}
}

func (op *MintOperation) Verify() error {
	if op == nil {
		return errNilMintOperation
	}

	return verify.All(&op.MintInput, &op.MintOutput, &op.TransferOutput)
}
