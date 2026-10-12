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
	_ UnsignedTx = (*CancelAutoRenewedValidatorStakeIncreaseTx)(nil)

	errMissingStakeIncreaseTxID = errors.New("missing stake increase tx id")
)

// CancelAutoRenewedValidatorStakeIncreaseTx cancels a pending stake increase
// of an auto-renewed validator. The stake locked by the referenced
// IncreaseAutoRenewedValidatorStakeTx is returned to the owners of its stake
// outputs. A stake increase can only be cancelled before it is applied to the
// validator's weight at the start of the validator's next cycle.
type CancelAutoRenewedValidatorStakeIncreaseTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// ID of the tx that created the auto-renewed validator.
	TxID ids.ID `serialize:"true" json:"txID"`

	// ID of the IncreaseAutoRenewedValidatorStakeTx to cancel.
	StakeIncreaseTxID ids.ID `serialize:"true" json:"stakeIncreaseTxID"`

	// Authorizes the stake increase of this validator to be cancelled.
	Auth verify.Verifiable `serialize:"true" json:"auth"`
}

func (tx *CancelAutoRenewedValidatorStakeIncreaseTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.TxID == ids.Empty:
		return errMissingTxID
	case tx.StakeIncreaseTxID == ids.Empty:
		return errMissingStakeIncreaseTxID
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

func (tx *CancelAutoRenewedValidatorStakeIncreaseTx) Visit(visitor TxVisitor) error {
	return visitor.CancelAutoRenewedValidatorStakeIncreaseTx(tx)
}
