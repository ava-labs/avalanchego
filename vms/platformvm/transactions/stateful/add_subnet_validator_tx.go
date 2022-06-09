// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ ProposalTx = &AddSubnetValidatorTx{}

type AddSubnetValidatorTx struct {
	*unsigned.AddSubnetValidatorTx

	txID        ids.ID // ID of signed add subnet validator tx
	signedBytes []byte // signed Tx bytes, needed to recreate signed.Tx
	creds       []verify.Verifiable

	verifier TxVerifier
}

// InitiallyPrefersCommit returns true if the proposed validators start time is
// after the current wall clock time,
func (tx *AddSubnetValidatorTx) InitiallyPrefersCommit() bool {
	clock := tx.verifier.Clock()
	return tx.StartTime().After(clock.Time())
}
