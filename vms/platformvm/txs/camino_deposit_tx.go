// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var _ UnsignedTx = (*DepositTx)(nil)

// DepositTx is an unsigned depositTx
type DepositTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of active offer that will be used for this deposit
	DepositOfferID ids.ID `serialize:"true" json:"depositOfferID"`
	// duration of deposit
	DepositDuration uint32 `serialize:"true" json:"duration"`
	// Where to send staking rewards when done validating
	RewardsOwner fx.Owner `serialize:"true" json:"rewardsOwner"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [DepositTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *DepositTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	tx.RewardsOwner.InitCtx(ctx)
}

func (tx *DepositTx) Duration() uint32 {
	return tx.DepositDuration
}

func (tx *DepositTx) DepositAmount() (uint64, error) {
	depositAmount := uint64(0)
	for _, out := range tx.Outs {
		if lockedOut, ok := out.Out.(*locked.Out); ok && lockedOut.IsNewlyLockedWith(locked.StateDeposited) {
			newDepositAmount, err := math.Add64(depositAmount, lockedOut.Amount())
			if err != nil {
				return 0, err
			}
			depositAmount = newDepositAmount
		}
	}
	return depositAmount, nil
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *DepositTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
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

func (tx *DepositTx) Visit(visitor Visitor) error {
	return visitor.DepositTx(tx)
}
