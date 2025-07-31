// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm/state"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	_ txs.Visitor = (*SemanticVerifier)(nil)

	errAssetIDMismatch = errors.New("asset IDs in the input don't match the utxo")
	errNotAnAsset      = errors.New("not an asset")
	errIncompatibleFx  = errors.New("incompatible feature extension")
	errUnknownFx       = errors.New("unknown feature extension")
)

type SemanticVerifier struct {
	*Backend
	State state.ReadOnlyChain
	Tx    *txs.Tx
}

func (v *SemanticVerifier) BaseTx(tx *txs.BaseTx) error {
	for i, in := range tx.Ins {
		// Note: Verification of the length of [t.tx.Creds] happens during
		// syntactic verification, which happens before semantic verification.
		cred := v.Tx.Creds[i].Credential
		if err := v.verifyTransfer(tx, in, cred); err != nil {
			return fmt.Errorf("failed to verify transfer: %w", err)
		}
	}

	for _, out := range tx.Outs {
		fxIndex, err := v.getFx(out.Out)
		if err != nil {
			return fmt.Errorf("failed to get fx: %w", err)
		}

		assetID := out.AssetID()
		if err := v.verifyFxUsage(fxIndex, assetID); err != nil {
			return fmt.Errorf("failed to verify fx usage: %w", err)
		}
	}

	return nil
}

func (v *SemanticVerifier) CreateAssetTx(tx *txs.CreateAssetTx) error {
	return v.BaseTx(&tx.BaseTx)
}

func (v *SemanticVerifier) OperationTx(tx *txs.OperationTx) error {
	if err := v.BaseTx(&tx.BaseTx); err != nil {
		return err
	}

	if !v.Bootstrapped || v.Tx.ID().String() == "MkvpJS13eCnEYeYi9B5zuWrU9goG9RBj7nr83U7BjrFV22a12" {
		return nil
	}

	offset := len(tx.Ins)
	for i, op := range tx.Ops {
		// Note: Verification of the length of [t.tx.Creds] happens during
		// syntactic verification, which happens before semantic verification.
		cred := v.Tx.Creds[i+offset].Credential
		if err := v.verifyOperation(tx, op, cred); err != nil {
			return err
		}
	}
	return nil
}

func (v *SemanticVerifier) ImportTx(tx *txs.ImportTx) error {
	if err := v.BaseTx(&tx.BaseTx); err != nil {
		return err
	}

	if !v.Bootstrapped {
		return nil
	}

	if err := verify.SameSubnet(context.TODO(), v.Ctx, tx.SourceChain); err != nil {
		return err
	}

	utxoIDs := make([][]byte, len(tx.ImportedIns))
	for i, in := range tx.ImportedIns {
		inputID := in.UTXOID.InputID()
		utxoIDs[i] = inputID[:]
	}

	allUTXOBytes, err := v.Ctx.SharedMemory.Get(tx.SourceChain, utxoIDs)
	if err != nil {
		return err
	}

	offset := len(tx.Ins)
	for i, in := range tx.ImportedIns {
		utxo := avax.UTXO{}
		if _, err := v.Codec.Unmarshal(allUTXOBytes[i], &utxo); err != nil {
			return err
		}

		// Note: Verification of the length of [t.tx.Creds] happens during
		// syntactic verification, which happens before semantic verification.
		cred := v.Tx.Creds[i+offset].Credential
		if err := v.verifyTransferOfUTXO(tx, in, cred, &utxo); err != nil {
			return err
		}
	}
	return nil
}

func (v *SemanticVerifier) ExportTx(tx *txs.ExportTx) error {
	if err := v.BaseTx(&tx.BaseTx); err != nil {
		return err
	}

	if v.Bootstrapped {
		if err := verify.SameSubnet(context.TODO(), v.Ctx, tx.DestinationChain); err != nil {
			return err
		}
	}

	for _, out := range tx.ExportedOuts {
		fxIndex, err := v.getFx(out.Out)
		if err != nil {
			return err
		}

		assetID := out.AssetID()
		if err := v.verifyFxUsage(fxIndex, assetID); err != nil {
			return err
		}
	}
	return nil
}

func (v *SemanticVerifier) verifyTransfer(
	tx txs.UnsignedTx,
	in *avax.TransferableInput,
	cred verify.Verifiable,
) error {
	utxo, err := v.State.GetUTXO(in.UTXOID.InputID())
	if err != nil {
		return fmt.Errorf("failed to get utxo %s: %w", in.UTXOID.InputID(), err)
	}
	return v.verifyTransferOfUTXO(tx, in, cred, utxo)
}

func (v *SemanticVerifier) verifyTransferOfUTXO(
	tx txs.UnsignedTx,
	in *avax.TransferableInput,
	cred verify.Verifiable,
	utxo *avax.UTXO,
) error {
	utxoAssetID := utxo.AssetID()
	inAssetID := in.AssetID()
	if utxoAssetID != inAssetID {
		return errAssetIDMismatch
	}

	fxIndex, err := v.getFx(cred)
	if err != nil {
		return err
	}

	if err := v.verifyFxUsage(fxIndex, inAssetID); err != nil {
		return err
	}

	fx := v.Fxs[fxIndex].Fx
	return fx.VerifyTransfer(tx, in.In, cred, utxo.Out)
}

func (v *SemanticVerifier) verifyOperation(
	tx *txs.OperationTx,
	op *txs.Operation,
	cred verify.Verifiable,
) error {
	var (
		opAssetID = op.AssetID()
		numUTXOs  = len(op.UTXOIDs)
		utxos     = make([]interface{}, numUTXOs)
	)
	for i, utxoID := range op.UTXOIDs {
		utxo, err := v.State.GetUTXO(utxoID.InputID())
		if err != nil {
			return err
		}

		utxoAssetID := utxo.AssetID()
		if utxoAssetID != opAssetID {
			return errAssetIDMismatch
		}
		utxos[i] = utxo.Out
	}

	fxIndex, err := v.getFx(op.Op)
	if err != nil {
		return err
	}

	if err := v.verifyFxUsage(fxIndex, opAssetID); err != nil {
		return err
	}

	fx := v.Fxs[fxIndex].Fx
	return fx.VerifyOperation(tx, op.Op, cred, utxos)
}

func (v *SemanticVerifier) verifyFxUsage(
	fxID int,
	assetID ids.ID,
) error {
	tx, err := v.State.GetTx(assetID)
	if err != nil {
		return err
	}

	createAssetTx, ok := tx.Unsigned.(*txs.CreateAssetTx)
	if !ok {
		return errNotAnAsset
	}

	for _, state := range createAssetTx.States {
		if state.FxIndex == uint32(fxID) {
			return nil
		}
	}

	return errIncompatibleFx
}

func (v *SemanticVerifier) getFx(val interface{}) (int, error) {
	valType := reflect.TypeOf(val)
	fx, exists := v.TypeToFxIndex[valType]
	if !exists {
		return 0, errUnknownFx
	}
	return fx, nil
}
