// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package nftfx

import (
	"errors"

	"github.com/MetalBlockchain/metalgo/snow"
	"github.com/MetalBlockchain/metalgo/vms/components/verify"
	"github.com/MetalBlockchain/metalgo/vms/secp256k1fx"
)

var errNilTransferOperation = errors.New("nil transfer operation")

type TransferOperation struct {
	Input  secp256k1fx.Input `serialize:"true" json:"input"`
	Output TransferOutput    `serialize:"true" json:"output"`
}

func (op *TransferOperation) InitCtx(ctx *snow.Context) {
	op.Output.OutputOwners.InitCtx(ctx)
}

func (op *TransferOperation) Cost() (uint64, error) {
	return op.Input.Cost()
}

func (op *TransferOperation) Outs() []verify.State {
	return []verify.State{&op.Output}
}

func (op *TransferOperation) Verify() error {
	if op == nil {
		return errNilTransferOperation
	}

	return verify.All(&op.Input, &op.Output)
}
