// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platform

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	_ UnsignedTx = (*CancelAutoRenewedValidatorStakeDecreaseTx)(nil)

	errMissingStakeDecreaseTxID = errors.New("missing stake decrease tx id")
)

// CancelAutoRenewedValidatorStakeDecreaseTx cancels a pending stake decrease
// of an auto-renewed validator, so that the stake requested by the referenced
// DecreaseAutoRenewedValidatorStakeTx stays staked. A stake decrease can only
// be cancelled before it is withdrawn at the start of the validator's next
// cycle.
type CancelAutoRenewedValidatorStakeDecreaseTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// ID of the tx that created the auto-renewed validator.
	TxID ids.ID `serialize:"true" json:"txID"`

	// ID of the DecreaseAutoRenewedValidatorStakeTx to cancel.
	StakeDecreaseTxID ids.ID `serialize:"true" json:"stakeDecreaseTxID"`

	// Authorizes the stake decrease of this validator to be cancelled.
	Auth verify.Verifiable `serialize:"true" json:"auth"`
}

func (tx *CancelAutoRenewedValidatorStakeDecreaseTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.TxID == ids.Empty:
		return errMissingTxID
	case tx.StakeDecreaseTxID == ids.Empty:
		return errMissingStakeDecreaseTxID
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("verifying BaseTx: %w", err)
	}

	if err := tx.Auth.Verify(); err != nil {
		return fmt.Errorf("verifying auth: %w", err)
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *CancelAutoRenewedValidatorStakeDecreaseTx) Visit(visitor TxVisitor) error {
	return visitor.CancelAutoRenewedValidatorStakeDecreaseTx(tx)
}
