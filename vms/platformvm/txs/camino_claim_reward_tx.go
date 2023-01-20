// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
)

var (
	_ UnsignedTx = (*ClaimRewardTx)(nil)

	errNoDeposits           = errors.New("no deposit txs to claim reward from")
	errNonUniqueDepositTxID = errors.New("non-unique deposit tx id")
)

// ClaimRewardTx is an unsigned claimRewardTx
type ClaimRewardTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// IDs of deposit txs which will be used to claim rewards
	DepositTxs []ids.ID `serialize:"true"`
	// Rewards will be minted to this owner, unless all of its fields has zero-values.
	// If it is empty, rewards will be minted for depositTx.RewardsOwner.
	RewardsOwner fx.Owner `serialize:"true" json:"rewardsOwner"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [ClaimRewardTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *ClaimRewardTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	tx.RewardsOwner.InitCtx(ctx)
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *ClaimRewardTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.DepositTxs) == 0:
		return errNoDeposits
	}

	uniqueDeposits := set.NewSet[ids.ID](len(tx.DepositTxs))
	for _, depositTxID := range tx.DepositTxs {
		if _, ok := uniqueDeposits[depositTxID]; ok {
			return errNonUniqueDepositTxID
		}
		uniqueDeposits.Add(depositTxID)
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}
	if err := verify.All(tx.RewardsOwner); err != nil {
		return fmt.Errorf("failed to verify validator or rewards owner: %w", err)
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *ClaimRewardTx) Visit(visitor Visitor) error {
	return visitor.ClaimRewardTx(tx)
}
