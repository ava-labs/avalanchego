// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var (
	_ UnsignedTx = (*ClaimTx)(nil)

	errNoDepositsOrClaimables = errors.New("no deposit txs with rewards or claimables to claim")
	errNonUniqueDepositTxID   = errors.New("non-unique deposit tx id")
	errNonUniqueOwnerID       = errors.New("non-unique owner id")
	errWrongClaimedAmount     = errors.New("zero claimed amount or amounts len doesn't match ownerIDs len")
)

// ClaimTx is an unsigned ClaimTx
type ClaimTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// IDs of deposit txs which will be used to claim rewards
	DepositTxIDs []ids.ID `serialize:"true" json:"depositTxIDs"`
	// Owner ids of claimables owners (validator rewards, expired deposit unclaimed rewards).
	// ID is hash256 of owners structure (secp256k1fx.OutputOwners, for example)
	ClaimableOwnerIDs []ids.ID `serialize:"true" json:"claimableOwnerIDs"`
	// How much tokens will be claimed for corresponding claimableOwnerIDs
	ClaimedAmounts []uint64 `serialize:"true" json:"claimedAmounts"`
	// Reward and claimables outputs will be minted to this owner, unless all of its fields has zero-values.
	// If it is empty, deposit rewards will be minted for depositTx.RewardsOwner
	// and claimables will be minted for claimable owners.
	ClaimTo fx.Owner `serialize:"true" json:"claimTo"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [ClaimTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *ClaimTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	tx.ClaimTo.InitCtx(ctx)
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *ClaimTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.DepositTxIDs) == 0 && len(tx.ClaimableOwnerIDs) == 0:
		return errNoDepositsOrClaimables
	case len(tx.ClaimableOwnerIDs) != len(tx.ClaimedAmounts):
		return errWrongClaimedAmount
	}

	uniqueIDs := set.NewSet[ids.ID](len(tx.DepositTxIDs))
	for _, depositTxID := range tx.DepositTxIDs {
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
	if err := tx.ClaimTo.Verify(); err != nil {
		return fmt.Errorf("failed to verify DepositRewardsOwner: %w", err)
	}

	if err := locked.VerifyNoLocks(tx.Ins, tx.Outs); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *ClaimTx) Visit(visitor Visitor) error {
	return visitor.ClaimTx(tx)
}
