// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ UnsignedTx = (*RewardsImportTx)(nil)

	errNotTreasuryOwner         = errors.New("not treasury owner")
	errProducedNotEqualConsumed = errors.New("produced amount not equal to consumed amount")
	errNotAVAXAsset             = errors.New("input assetID isn't avax assetID")
	errWrongOutsNumber          = errors.New("wrong number of outputs")
)

// RewardsImportTx is an unsigned rewardsImportTx
type RewardsImportTx struct {
	// Metadata, imported inputs and resulting output
	BaseTx
}

func (tx *RewardsImportTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.Outs) != 1:
		return fmt.Errorf("expect 1 output, but got %d: %w", len(tx.Outs), errWrongOutsNumber)
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	out := tx.Outs[0]

	if outAssetID := out.AssetID(); outAssetID != ctx.AVAXAssetID {
		return fmt.Errorf("output 0 has asset ID %s but expect %s: %w",
			outAssetID, ctx.AVAXAssetID, errNotAVAXAsset)
	}

	secpOut, ok := out.Out.(*secp256k1fx.TransferOutput)
	if !ok {
		return locked.ErrWrongOutType
	}

	if !secpOut.OutputOwners.Equals(treasury.Owner) {
		return errNotTreasuryOwner
	}

	importedAmount := uint64(0)
	for i, in := range tx.Ins {
		if inputAssetID := in.AssetID(); inputAssetID != ctx.AVAXAssetID {
			return fmt.Errorf("input %d has asset ID %s but expect %s: %w",
				i, inputAssetID, ctx.AVAXAssetID, errNotAVAXAsset)
		}

		newImportedAmount, err := math.Add64(importedAmount, in.In.Amount())
		if err != nil {
			return err
		}
		importedAmount = newImportedAmount
	}

	if out.Out.Amount() != importedAmount {
		return errProducedNotEqualConsumed
	}

	if err := locked.VerifyNoLocks(tx.Ins, nil); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true

	return nil
}

func (tx *RewardsImportTx) Visit(visitor Visitor) error {
	return visitor.RewardsImportTx(tx)
}
