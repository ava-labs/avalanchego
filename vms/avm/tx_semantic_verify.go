// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"

	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var _ txs.Visitor = (*txSemanticVerify)(nil)

// SemanticVerify that this transaction is well-formed.
type txSemanticVerify struct {
	tx *txs.Tx
	vm *VM
}

func (t *txSemanticVerify) BaseTx(tx *txs.BaseTx) error {
	for i, in := range tx.Ins {
		// Note: Verification of the length of [t.tx.Creds] happens during
		// syntactic verification, which happens before semantic verification.
		cred := t.tx.Creds[i].Verifiable
		if err := t.vm.verifyTransfer(t.tx.Unsigned, in, cred); err != nil {
			return err
		}
	}

	for _, out := range tx.Outs {
		fxIndex, err := t.vm.getFx(out.Out)
		if err != nil {
			return err
		}

		if assetID := out.AssetID(); !t.vm.verifyFxUsage(fxIndex, assetID) {
			return errIncompatibleFx
		}
	}
	return nil
}

func (t *txSemanticVerify) ImportTx(tx *txs.ImportTx) error {
	if err := t.BaseTx(&tx.BaseTx); err != nil {
		return err
	}

	if !t.vm.bootstrapped {
		return nil
	}

	if err := verify.SameSubnet(context.TODO(), t.vm.ctx, tx.SourceChain); err != nil {
		return err
	}

	utxoIDs := make([][]byte, len(tx.ImportedIns))
	for i, in := range tx.ImportedIns {
		inputID := in.UTXOID.InputID()
		utxoIDs[i] = inputID[:]
	}

	allUTXOBytes, err := t.vm.ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
	if err != nil {
		return err
	}

	codec := t.vm.parser.Codec()
	offset := tx.BaseTx.NumCredentials()
	for i, in := range tx.ImportedIns {
		utxo := avax.UTXO{}
		if _, err := codec.Unmarshal(allUTXOBytes[i], &utxo); err != nil {
			return err
		}

		// Note: Verification of the length of [t.tx.Creds] happens during
		// syntactic verification, which happens before semantic verification.
		cred := t.tx.Creds[i+offset].Verifiable
		if err := t.vm.verifyTransferOfUTXO(tx, in, cred, &utxo); err != nil {
			return err
		}
	}
	return nil
}

func (t *txSemanticVerify) ExportTx(tx *txs.ExportTx) error {
	if t.vm.bootstrapped {
		if err := verify.SameSubnet(context.TODO(), t.vm.ctx, tx.DestinationChain); err != nil {
			return err
		}
	}

	for _, out := range tx.ExportedOuts {
		fxIndex, err := t.vm.getFx(out.Out)
		if err != nil {
			return err
		}

		assetID := out.AssetID()
		if !t.vm.verifyFxUsage(fxIndex, assetID) {
			return errIncompatibleFx
		}
	}

	return t.BaseTx(&tx.BaseTx)
}

func (t *txSemanticVerify) OperationTx(tx *txs.OperationTx) error {
	if err := t.BaseTx(&tx.BaseTx); err != nil {
		return err
	}

	offset := tx.BaseTx.NumCredentials()
	for i, op := range tx.Ops {
		// Note: Verification of the length of [t.tx.Creds] happens during
		// syntactic verification, which happens before semantic verification.
		cred := t.tx.Creds[i+offset].Verifiable
		if err := t.vm.verifyOperation(tx, op, cred); err != nil {
			return err
		}
	}
	return nil
}

func (t *txSemanticVerify) CreateAssetTx(tx *txs.CreateAssetTx) error {
	return t.BaseTx((&tx.BaseTx))
}
