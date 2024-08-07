// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"fmt"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

var _ UnsignedTx = (*UnlockDepositTx)(nil)

// UnlockDepositTx is an unsigned unlockDepositTx
type UnlockDepositTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
}

// SyntacticVerify returns nil if [tx] is valid
func (tx *UnlockDepositTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	if err := locked.VerifyLockMode(tx.Ins, tx.Outs, true); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *UnlockDepositTx) Visit(visitor Visitor) error {
	return visitor.UnlockDepositTx(tx)
}
