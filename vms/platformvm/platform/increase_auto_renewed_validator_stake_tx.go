// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platform

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var _ UnsignedTx = (*IncreaseAutoRenewedValidatorStakeTx)(nil)

// IncreaseAutoRenewedValidatorStakeTx locks additional stake for an
// auto-renewed validator. The stake is locked when the tx is accepted, but it
// is only added to the validator's weight at the start of the validator's next
// cycle, so the current cycle is not affected.
type IncreaseAutoRenewedValidatorStakeTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// ID of the tx that created the auto-renewed validator.
	TxID ids.ID `serialize:"true" json:"txID"`

	// Authorizes the stake of this validator to be increased.
	Auth verify.Verifiable `serialize:"true" json:"auth"`

	// Where to send the locked stake when the validator stops validating.
	StakeOuts []*avax.TransferableOutput `serialize:"true" json:"stake"`
}

func (tx *IncreaseAutoRenewedValidatorStakeTx) Stake() []*avax.TransferableOutput {
	return tx.StakeOuts
}

// Weight returns the amount of stake locked by this tx.
//
// The error from sumStakeOuts is intentionally dropped: this method must only
// be called on a syntactically verified tx (SyntacticVerify validates the stake
// outputs and is run before acceptance, so persisted txs satisfy this too).
// Under that invariant the error is always nil.
func (tx *IncreaseAutoRenewedValidatorStakeTx) Weight() uint64 {
	weight, _ := sumStakeOuts(tx.StakeOuts)
	return weight
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// IncreaseAutoRenewedValidatorStakeTx. Also sets the ctx to the given vm.ctx so
// that the addresses can be json marshalled into human readable format
func (tx *IncreaseAutoRenewedValidatorStakeTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	for _, out := range tx.StakeOuts {
		out.FxID = secp256k1fx.ID
		out.InitCtx(ctx)
	}
}

func (tx *IncreaseAutoRenewedValidatorStakeTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.TxID == ids.Empty:
		return errMissingTxID
	case len(tx.StakeOuts) == 0:
		return errNoStake
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("verifying BaseTx: %w", err)
	}

	if err := tx.Auth.Verify(); err != nil {
		return fmt.Errorf("verifying auth: %w", err)
	}

	for _, out := range tx.StakeOuts {
		if err := out.Verify(); err != nil {
			return fmt.Errorf("verifying output: %w", err)
		}
	}

	for _, out := range tx.StakeOuts {
		if out.AssetID() != ctx.AVAXAssetID {
			return errInvalidStakedAsset
		}
	}

	if _, err := sumStakeOuts(tx.StakeOuts); err != nil {
		return err
	}

	if !avax.IsSortedTransferableOutputs(tx.StakeOuts, Codec) {
		return errOutputsNotSorted
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *IncreaseAutoRenewedValidatorStakeTx) Visit(visitor TxVisitor) error {
	return visitor.IncreaseAutoRenewedValidatorStakeTx(tx)
}
