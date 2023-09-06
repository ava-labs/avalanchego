// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var _ UnsignedTx = (*StopStakerTx)(nil)

type StopStakerTx struct {
	BaseTx `serialize:"true"`

	// ID of the tx that created the staker being removed
	TxID ids.ID `serialize:"true" json:"txID"`

	// Proves that the issuer has the right to stop staking.
	StakerAuth verify.Verifiable `serialize:"true" json:"stakerAuthorization"`
}

func (tx *StopStakerTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	if err := tx.StakerAuth.Verify(); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *StopStakerTx) Visit(visitor Visitor) error {
	return visitor.StopStakerTx(tx)
}
