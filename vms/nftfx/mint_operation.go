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

package nftfx

import (
	"errors"

	"github.com/chain4travel/caminogo/vms/types"

	"github.com/chain4travel/caminogo/snow"
	"github.com/chain4travel/caminogo/vms/components/verify"
	"github.com/chain4travel/caminogo/vms/secp256k1fx"
)

var errNilMintOperation = errors.New("nil mint operation")

type MintOperation struct {
	MintInput secp256k1fx.Input           `serialize:"true" json:"mintInput"`
	GroupID   uint32                      `serialize:"true" json:"groupID"`
	Payload   types.JSONByteSlice         `serialize:"true" json:"payload"`
	Outputs   []*secp256k1fx.OutputOwners `serialize:"true" json:"outputs"`
}

func (op *MintOperation) InitCtx(ctx *snow.Context) {
	for _, out := range op.Outputs {
		out.InitCtx(ctx)
	}
}

func (op *MintOperation) Cost() (uint64, error) {
	return op.MintInput.Cost()
}

// Outs Returns []TransferOutput as []verify.State
func (op *MintOperation) Outs() []verify.State {
	outs := []verify.State{}
	for _, out := range op.Outputs {
		outs = append(outs, &TransferOutput{
			GroupID:      op.GroupID,
			Payload:      op.Payload,
			OutputOwners: *out,
		})
	}
	return outs
}

func (op *MintOperation) Verify() error {
	switch {
	case op == nil:
		return errNilMintOperation
	case len(op.Payload) > MaxPayloadSize:
		return errPayloadTooLarge
	}

	for _, out := range op.Outputs {
		if err := out.Verify(); err != nil {
			return err
		}
	}
	return op.MintInput.Verify()
}
