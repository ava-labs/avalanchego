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

package avm

import (
	"errors"

	"github.com/chain4travel/caminogo/codec"
	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow"
	"github.com/chain4travel/caminogo/vms/components/avax"
	"github.com/chain4travel/caminogo/vms/components/verify"
)

var (
	errNilTx = errors.New("nil tx is not valid")

	_ UnsignedTx = &BaseTx{}
)

// BaseTx is the basis of all transactions.
type BaseTx struct {
	avax.BaseTx `serialize:"true"`
}

// Init sets the FxID fields in the inputs and outputs of this [BaseTx]
// Also sets the [ctx] in the OutputOwners to the given [vm.ctx] so that the
// addresses can be json marshalled into human readable format
func (t *BaseTx) Init(vm *VM) error {
	for _, in := range t.Ins {
		fx, err := vm.getParsedFx(in.In)
		if err != nil {
			return err
		}
		in.FxID = fx.ID
	}

	for _, out := range t.Outs {
		fx, err := vm.getParsedFx(out.Out)
		if err != nil {
			return err
		}
		out.FxID = fx.ID
		out.InitCtx(vm.ctx)
	}
	return nil
}

// SyntacticVerify that this transaction is well-formed.
func (t *BaseTx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Manager,
	txFeeAssetID ids.ID,
	txFee uint64,
	_ uint64,
	_ int,
) error {
	if t == nil {
		return errNilTx
	}
	if err := t.MetadataVerify(ctx); err != nil {
		return err
	}

	return avax.VerifyTx(
		txFee,
		txFeeAssetID,
		[][]*avax.TransferableInput{t.Ins},
		[][]*avax.TransferableOutput{t.Outs},
		c,
	)
}

// SemanticVerify that this transaction is valid to be spent.
func (t *BaseTx) SemanticVerify(vm *VM, tx UnsignedTx, creds []verify.Verifiable) error {
	for i, in := range t.Ins {
		cred := creds[i]
		if err := vm.verifyTransfer(tx, in, cred); err != nil {
			return err
		}
	}
	for _, out := range t.Outs {
		fxIndex, err := vm.getFx(out.Out)
		if err != nil {
			return err
		}
		if assetID := out.AssetID(); !vm.verifyFxUsage(fxIndex, assetID) {
			return errIncompatibleFx
		}
	}
	return nil
}

// ExecuteWithSideEffects writes the batch with any additional side effects
func (t *BaseTx) ExecuteWithSideEffects(_ *VM, batch database.Batch) error { return batch.Write() }
