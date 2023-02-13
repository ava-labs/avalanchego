// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ UnsignedTx = (*RewardsImportTx)(nil)

	errNotTreasuryOwner         = errors.New("not treasury owner")
	errProducedNotEqualConsumed = errors.New("produced amount not equal to consumed amount")
	errNotAVAXAsset             = errors.New("input assetID isn't avax assetID")
)

// RewardsImportTx is an unsigned rewardsImportTx
type RewardsImportTx struct {
	// Inputs that consume UTXOs produced on the chain
	ImportedInputs []*avax.TransferableInput `serialize:"true" json:"importedInputs"`
	// Output containing all imported tokens
	Out *avax.TransferableOutput `serialize:"true" json:"output"`

	SyntacticallyVerified bool

	unsignedBytes []byte // Unsigned byte representation of this data
}

func (tx *RewardsImportTx) Initialize(unsignedBytes []byte) {
	tx.unsignedBytes = unsignedBytes
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [RewardsImportTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *RewardsImportTx) InitCtx(ctx *snow.Context) {
	for _, in := range tx.ImportedInputs {
		in.FxID = secp256k1fx.ID
	}
	tx.Out.FxID = secp256k1fx.ID
	tx.Out.InitCtx(ctx)
}

func (tx *RewardsImportTx) Bytes() []byte {
	return tx.unsignedBytes
}

func (tx *RewardsImportTx) InputIDs() set.Set[ids.ID] {
	inputIDs := set.NewSet[ids.ID](len(tx.ImportedInputs))
	for _, in := range tx.ImportedInputs {
		inputIDs.Add(in.InputID())
	}
	return inputIDs
}

func (tx *RewardsImportTx) Outputs() []*avax.TransferableOutput {
	return []*avax.TransferableOutput{tx.Out}
}

func (tx *RewardsImportTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.Out.Verify(); err != nil {
		return fmt.Errorf("output failed verification: %w", err)
	}

	secpOut, ok := tx.Out.Out.(*secp256k1fx.TransferOutput)
	if !ok {
		return locked.ErrWrongOutType
	}

	if !secpOut.OutputOwners.Equals(treasury.Owner) {
		return errNotTreasuryOwner
	}

	importedAmount := uint64(0)
	for i, in := range tx.ImportedInputs {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("input failed verification: %w", err)
		}

		if inputAssetID := in.AssetID(); inputAssetID != ctx.AVAXAssetID {
			return fmt.Errorf(
				"input %d has asset ID %s but expect %s: %w",
				i,
				inputAssetID,
				ctx.AVAXAssetID,
				errNotAVAXAsset,
			)
		}

		newImportedAmount, err := math.Add64(importedAmount, in.In.Amount())
		if err != nil {
			return err
		}
		importedAmount = newImportedAmount
	}

	if err := locked.VerifyNoLocks(tx.ImportedInputs, nil); err != nil {
		return err
	}

	switch {
	case tx.Out.Out.Amount() != importedAmount:
		return errProducedNotEqualConsumed
	case !utils.IsSortedAndUniqueSortable(tx.ImportedInputs):
		return errInputsNotSortedUnique
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true

	return nil
}

func (tx *RewardsImportTx) Visit(visitor Visitor) error {
	return visitor.RewardsImportTx(tx)
}
