// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var (
	_ UnsignedTx = (*RewardsImportTx)(nil)

	errNotAVAXAsset    = errors.New("transferable assetID isn't avax assetID")
	errWrongOutsNumber = errors.New("wrong number of outputs")
)

// RewardsImportTx is an unsigned rewardsImportTx
type RewardsImportTx struct {
	// Metadata, imported inputs
	BaseTx `serialize:"true"`
}

func (tx *RewardsImportTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.Outs) != 0:
		return fmt.Errorf("expect 0 outputs, but got %d: %w", len(tx.Outs), errWrongOutsNumber)
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
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
