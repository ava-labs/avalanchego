// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var (
	_ ProposalTx = &AddDelegatorTx{}

	ErrOverDelegated = errors.New("validator would be over delegated")
)

// maxValidatorWeightFactor is the maximum factor of the validator stake
// that is allowed to be placed on a validator.
const maxValidatorWeightFactor uint64 = 5

type AddDelegatorTx struct {
	*unsigned.AddDelegatorTx

	txID        ids.ID // ID of signed add subnet validator tx
	signedBytes []byte // signed Tx bytes, needed to recreate signed.Tx
	creds       []verify.Verifiable

	verifier TxVerifier
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *AddDelegatorTx) InitiallyPrefersCommit() bool {
	clock := tx.verifier.Clock()
	return tx.StartTime().After(clock.Time())
}
