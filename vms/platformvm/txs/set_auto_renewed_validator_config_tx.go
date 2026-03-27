// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

type SetAutoRenewedValidatorConfigTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// ID of the tx that created the auto-renewed validator.
	TxID ids.ID `serialize:"true" json:"txID"`

	// Authorizes this validator to be updated.
	Auth verify.Verifiable `serialize:"true" json:"auth"`

	// Percentage of rewards to restake at the end of each cycle, expressed in millionths (percentage * 10,000).
	// Range [0..1_000_000]:
	//   0         = restake principal only; withdraw 100% of rewards
	//   300_000   = restake 30% of rewards; withdraw 70%
	//   1_000_000 = restake 100% of rewards; withdraw 0%
	AutoCompoundRewardShares uint32 `serialize:"true" json:"autoCompoundRewardShares"`

	// Period for the next cycle (in seconds). Takes effect at cycle end.
	// If 0, stop at the end of the current cycle and unlock funds.
	Period uint64 `serialize:"true" json:"period"`
}

func (tx *SetAutoRenewedValidatorConfigTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.TxID == ids.Empty:
		return errMissingTxID
	case tx.AutoCompoundRewardShares > reward.PercentDenominator:
		return errTooManyAutoCompoundRewardShares
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}

	if err := tx.Auth.Verify(); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *SetAutoRenewedValidatorConfigTx) Visit(visitor Visitor) error {
	return visitor.SetAutoRenewedValidatorConfigTx(tx)
}
