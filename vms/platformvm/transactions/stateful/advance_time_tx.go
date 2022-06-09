// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var _ ProposalTx = &AdvanceTimeTx{}

// SyncBound is the synchrony bound used for safe decision making
const SyncBound = 10 * time.Second

type AdvanceTimeTx struct {
	*unsigned.AdvanceTimeTx

	ID    ids.ID // ID of signed advance time tx
	creds []verify.Verifiable

	verifier TxVerifier
}

// InitiallyPrefersCommit returns true if the proposed time is at
// or before the current time plus the synchrony bound
func (tx *AdvanceTimeTx) InitiallyPrefersCommit() bool {
	clock := tx.verifier.Clock()
	txTimestamp := tx.Timestamp()
	localTimestamp := clock.Time()
	localTimestampPlusSync := localTimestamp.Add(SyncBound)
	return !txTimestamp.After(localTimestampPlusSync)
}
