// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
)

var (
	errMissingTxID     = errors.New("missing tx id")
	errNoUpdatedFields = errors.New("no updated fields")
)

type SetAutoRestakeConfigTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`

	// ID of the tx that created the continuous validator.
	TxID ids.ID `serialize:"true" json:"txID"`

	// Authorizes this validator to be updated.
	Auth verify.Verifiable `serialize:"true" json:"auth"`

	// Optionally update the auto-restake shares, expressed in percentage, times 10,000.
	// If nil, leave unchanged. If provided:
	//   0         = restake principal only; withdraw 100% of rewards
	//   300_000   = restake 30% of rewards; withdraw 70%
	//   1_000_000 = restake 100% of rewards; withdraw 0%
	AutoRestakeShares    uint32 `serialize:"true" json:"autoRestakeShares"`
	HasAutoRestakeShares bool   `serialize:"true" json:"hasAutoRestakeShares"`

	// Optionally update the period for the next cycle (in seconds). Takes effect at cycle end.
	// If nil, leave unchanged.
	// If 0, stop at the end of the current cycle and unlock funds.
	Period    uint64 `serialize:"true" json:"period"`
	HasPeriod bool   `serialize:"true" json:"hasPeriod"`
}

func (tx *SetAutoRestakeConfigTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.TxID == ids.Empty:
		return errMissingTxID
	case !tx.HasPeriod && !tx.HasAutoRestakeShares:
		return errNoUpdatedFields
	case tx.HasAutoRestakeShares && tx.AutoRestakeShares > reward.PercentDenominator:
		return errTooManyRestakeShares
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

func (tx *SetAutoRestakeConfigTx) Visit(visitor Visitor) error {
	return visitor.SetAutoRestakeConfigTx(tx)
}
