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
	_ UnsignedTx = (*DecreaseAutoRenewedValidatorStakeTx)(nil)

	errZeroStakeDecrease = errors.New("stake decrease must be greater than 0")
)

// DecreaseAutoRenewedValidatorStakeTx withdraws stake from an auto-renewed
// validator at the start of its next cycle. The withdrawn stake is returned to
// the owners of the stake that it is drawn from.
type DecreaseAutoRenewedValidatorStakeTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// ID of the tx that created the auto-renewed validator.
	TxID ids.ID `serialize:"true" json:"txID"`

	// Authorizes the stake of this validator to be decreased.
	Auth verify.Verifiable `serialize:"true" json:"auth"`

	// Amount of stake to withdraw, in nAVAX.
	Amount uint64 `serialize:"true" json:"amount"`
}

func (tx *DecreaseAutoRenewedValidatorStakeTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.TxID == ids.Empty:
		return errMissingTxID
	case tx.Amount == 0:
		return errZeroStakeDecrease
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

func (tx *DecreaseAutoRenewedValidatorStakeTx) Visit(visitor TxVisitor) error {
	return visitor.DecreaseAutoRenewedValidatorStakeTx(tx)
}
