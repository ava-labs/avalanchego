// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

var (
	_ UnsignedTx = (*IncreaseL1ValidatorBalanceTx)(nil)

	ErrZeroBalance = errors.New("balance must be greater than 0")
)

type IncreaseL1ValidatorBalanceTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID corresponding to the validator
	ValidationID ids.ID `serialize:"true" json:"validationID"`
	// Balance <= sum($AVAX inputs) - sum($AVAX outputs) - TxFee
	Balance uint64 `serialize:"true" json:"balance"`
}

func (tx *IncreaseL1ValidatorBalanceTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.Balance == 0:
		return ErrZeroBalance
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *IncreaseL1ValidatorBalanceTx) Visit(visitor Visitor) error {
	return visitor.IncreaseL1ValidatorBalanceTx(tx)
}
