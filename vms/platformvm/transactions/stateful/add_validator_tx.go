// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var (
	_ ProposalTx = &AddValidatorTx{}

	ErrWeightTooLarge            = errors.New("weight of this validator is too large")
	ErrStakeTooShort             = errors.New("staking period is too short")
	ErrStakeTooLong              = errors.New("staking period is too long")
	ErrFutureStakeTime           = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	ErrInsufficientDelegationFee = errors.New("staker charges an insufficient delegation fee")
)

// Maximum future start time for staking/delegating
const MaxFutureStartTime = 24 * 7 * 2 * time.Hour

type AddValidatorTx struct {
	*unsigned.AddValidatorTx

	txID        ids.ID // ID of signed add validator tx
	signedBytes []byte // signed Tx bytes, needed to recreate signed.Tx
	creds       []verify.Verifiable

	verifier TxVerifier
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *AddValidatorTx) InitiallyPrefersCommit() bool {
	clock := tx.verifier.Clock()
	return tx.StartTime().After(clock.Time())
}
