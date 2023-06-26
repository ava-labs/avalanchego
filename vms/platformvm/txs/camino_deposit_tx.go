// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var (
	_ UnsignedTx = (*DepositTx)(nil)

	errTooBigDeposit         = errors.New("to big deposit")
	errInvalidRewardOwner    = errors.New("invalid reward owner")
	errBadOfferOwnerAuth     = errors.New("bad offer owner auth")
	errBadDepositCreatorAuth = errors.New("bad deposit creator auth")
)

// DepositTx is an unsigned depositTx
type DepositTx struct {
	UpgradeVersionID codec.UpgradeVersionID
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of active offer that will be used for this deposit
	DepositOfferID ids.ID `serialize:"true" json:"depositOfferID"`
	// duration of deposit
	DepositDuration uint32 `serialize:"true" json:"duration"`
	// Where to send staking rewards when done validating
	RewardsOwner fx.Owner `serialize:"true" json:"rewardsOwner"`

	// Address that is authorized to create deposit with given offer. Could be empty, if offer owner is empty.
	DepositCreatorAddress ids.ShortID `serialize:"true" json:"depositCreator" upgradeVersion:"1"`
	// Auth for deposit creator address
	DepositCreatorAuth verify.Verifiable `serialize:"true" json:"depositCreatorAuth" upgradeVersion:"1"`
	// Auth for deposit offer owner
	DepositOfferOwnerAuth verify.Verifiable `serialize:"true" json:"ownerAuth" upgradeVersion:"1"`

	depositAmount *uint64
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [DepositTx]. Also sets the [ctx] to the given [vm.ctx] so that
// the addresses can be json marshalled into human readable format
func (tx *DepositTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	tx.RewardsOwner.InitCtx(ctx)
}

func (tx *DepositTx) DepositAmount() uint64 {
	if tx.depositAmount == nil {
		depositAmount := uint64(0)
		for _, out := range tx.Outs {
			if lockedOut, ok := out.Out.(*locked.Out); ok && lockedOut.IsNewlyLockedWith(locked.StateDeposited) {
				depositAmount += lockedOut.Amount()
			}
		}
		tx.depositAmount = &depositAmount
	}
	return *tx.depositAmount
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
	if err := tx.RewardsOwner.Verify(); err != nil {
		return fmt.Errorf("%w: %s", errInvalidRewardOwner, err)
	}

	depositAmount := uint64(0)
	for _, out := range tx.Outs {
		if lockedOut, ok := out.Out.(*locked.Out); ok && lockedOut.IsNewlyLockedWith(locked.StateDeposited) {
			newDepositAmount, err := math.Add64(depositAmount, lockedOut.Amount())
			if err != nil {
				return fmt.Errorf("%w: %s", errTooBigDeposit, err)
			}
			depositAmount = newDepositAmount
		}
	}
	tx.depositAmount = &depositAmount

	if tx.UpgradeVersionID.Version() > 0 {
		if err := tx.DepositCreatorAuth.Verify(); err != nil {
			return fmt.Errorf("%w: %s", errBadDepositCreatorAuth, err)
		}
		if err := tx.DepositOfferOwnerAuth.Verify(); err != nil {
			return errBadOfferOwnerAuth
		}
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *DepositTx) Visit(visitor Visitor) error {
	return visitor.DepositTx(tx)
}
