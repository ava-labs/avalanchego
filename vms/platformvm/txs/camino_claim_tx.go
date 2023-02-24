// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
	"github.com/ava-labs/avalanchego/vms/platformvm/treasury"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ UnsignedTx = (*ClaimTx)(nil)

	errNoDepositsOrClaimables     = errors.New("no deposit txs with rewards or claimables to claim")
	errNonUniqueDepositTxID       = errors.New("non-unique deposit tx id")
	errNonUniqueOwnerID           = errors.New("non-unique owner id")
	errWrongClaimedAmount         = errors.New("zero claimed amount or amounts len doesn't match ownerIDs len")
	errMultipleTreasuryOuts       = errors.New("multiple treasury outputs")
	errWrongProducedClaimedAmount = errors.New("claimed amount not equal to produced claimed amount")
)

// ClaimTx is an unsigned ClaimTx
type ClaimTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// IDs of deposit txs which will be used to claim rewards
	DepositTxs []ids.ID `serialize:"true" json:"depositTxs"`
	// Owner ids of claimables owners (validator rewards, expired deposit unclaimed rewards).
	// ID is hash256 of owners structure (secp256k1fx.OutputOwners, for example)
	ClaimableOwnerIDs []ids.ID `serialize:"true" json:"claimableOwnerIDs"`
	// How much tokens will be claimed for corresponding claimableOwnerIDs
	ClaimedAmount []uint64 `serialize:"true" json:"claimedAmount"`
	// Inputs that will consume treasury utxos
	ClaimableIns []*avax.TransferableInput `serialize:"true" json:"claimableIns"`
	// Outputs produced from treasury utxos containing both treasury change (if any) and claimed tokens
	ClaimableOuts []*avax.TransferableOutput `serialize:"true" json:"claimableOuts"`
	// Deposit rewards outputs will be minted to this owner, unless all of its fields has zero-values.
	// If it is empty, deposit rewards will be minted for depositTx.RewardsOwner.
	DepositRewardsOwner fx.Owner `serialize:"true" json:"depositRewardsOwner"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [ClaimTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *ClaimTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	tx.DepositRewardsOwner.InitCtx(ctx)
	for _, in := range tx.ClaimableIns {
		in.FxID = secp256k1fx.ID
	}
	for _, out := range tx.ClaimableOuts {
		out.FxID = secp256k1fx.ID
		out.InitCtx(ctx)
	}
}

func (tx *ClaimTx) InputIDs() set.Set[ids.ID] {
	inputIDs := set.NewSet[ids.ID](len(tx.Ins) + len(tx.ClaimableIns))
	for _, in := range tx.Ins {
		inputIDs.Add(in.InputID())
	}
	for _, in := range tx.ClaimableIns {
		inputIDs.Add(in.InputID())
	}
	return inputIDs
}

func (tx *ClaimTx) Outputs() []*avax.TransferableOutput {
	return append(tx.Outs, tx.ClaimableOuts...)
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *ClaimTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.DepositTxs) == 0 && len(tx.ClaimableOwnerIDs) == 0:
		return errNoDepositsOrClaimables
	case len(tx.ClaimableOwnerIDs) != len(tx.ClaimedAmount):
		return errWrongClaimedAmount
	case len(tx.ClaimableOwnerIDs) == 0 && len(tx.ClaimableIns) != 0:
		return errWrongClaimedAmount
	}

	claimedAmount := uint64(0)
	for _, amt := range tx.ClaimedAmount {
		if amt == 0 {
			return errWrongClaimedAmount
		}
		newClaimedAmount, err := math.Add64(claimedAmount, amt)
		if err != nil {
			return err
		}
		claimedAmount = newClaimedAmount
	}

	uniqueIDs := set.NewSet[ids.ID](len(tx.DepositTxs))
	for _, depositTxID := range tx.DepositTxs {
		if _, ok := uniqueIDs[depositTxID]; ok {
			return errNonUniqueDepositTxID
		}
		uniqueIDs.Add(depositTxID)
	}

	uniqueIDs = set.NewSet[ids.ID](len(tx.ClaimableOwnerIDs))
	for _, ownerID := range tx.ClaimableOwnerIDs {
		if _, ok := uniqueIDs[ownerID]; ok {
			return errNonUniqueOwnerID
		}
		uniqueIDs.Add(ownerID)
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	if err := tx.DepositRewardsOwner.Verify(); err != nil {
		return fmt.Errorf("failed to verify DepositRewardsOwner: %w", err)
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	consumedTreasury := uint64(0)
	for i, in := range tx.ClaimableIns {
		if err := in.Verify(); err != nil {
			return fmt.Errorf("failed to verify tx.ClaimableIns[%d]: %w", i, err)
		}
		if inputAssetID := in.AssetID(); inputAssetID != ctx.AVAXAssetID {
			return fmt.Errorf("input %d has asset ID %s but expect %s: %w",
				i, inputAssetID, ctx.AVAXAssetID, errNotAVAXAsset)
		}

		if _, ok := in.In.(*secp256k1fx.TransferInput); !ok {
			return locked.ErrWrongInType
		}

		newConsumedTreasury, err := math.Add64(consumedTreasury, in.In.Amount())
		if err != nil {
			return err
		}
		consumedTreasury = newConsumedTreasury
	}

	producedClaimed := uint64(0)
	producedTreasury := uint64(0)
	for i, out := range tx.ClaimableOuts {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("failed to verify tx.ClaimableOuts[%d]: %w", i, err)
		}
		if outputAssetID := out.AssetID(); outputAssetID != ctx.AVAXAssetID {
			return fmt.Errorf("output %d has asset ID %s but expect %s: %w",
				i, outputAssetID, ctx.AVAXAssetID, errNotAVAXAsset)
		}

		secpOut, ok := out.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			return locked.ErrWrongOutType
		}

		if secpOut.OutputOwners.Equals(treasury.Owner) {
			if producedTreasury != 0 {
				return errMultipleTreasuryOuts
			}
			newProducedTreasury, err := math.Add64(producedTreasury, out.Out.Amount())
			if err != nil {
				return err
			}
			producedTreasury = newProducedTreasury
		} else {
			newProducedClaimed, err := math.Add64(producedClaimed, out.Out.Amount())
			if err != nil {
				return err
			}
			producedClaimed = newProducedClaimed
		}
	}

	if producedClaimed != claimedAmount {
		return errWrongProducedClaimedAmount
	}

	producedFromTreasury, err := math.Add64(producedTreasury, producedClaimed)
	if err != nil {
		return err
	}
	// consumedTreasury-producedTreasury != producedClaimed
	if consumedTreasury != producedFromTreasury {
		return errProducedNotEqualConsumed
	}

	switch {
	case !avax.IsSortedTransferableOutputs(tx.ClaimableOuts, Codec):
		return errOutputsNotSorted
	case !utils.IsSortedAndUniqueSortable(tx.ClaimableIns):
		return errInputsNotSortedUnique
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *ClaimTx) Visit(visitor Visitor) error {
	return visitor.ClaimTx(tx)
}
