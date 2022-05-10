// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

// unsigned.UnsignedTx is an unsigned transaction
type Tx interface {
	unsigned.Tx

	// Attempts to verify this transaction with the provided txstate.
	SemanticVerify(verifier TxVerifier, parentState state.Mutable, creds []verify.Verifiable) error
}

// DecisionTx is an unsigned operation that can be immediately decided
type DecisionTx interface {
	Tx

	// Execute this transaction with the provided txstate.
	Execute(verifier TxVerifier, vs state.Versioned, creds []verify.Verifiable) (
		onAcceptFunc func() error,
		err error,
	)

	// To maintain consistency with the Atomic txs
	InputUTXOs() ids.Set

	// AtomicOperations provides the requests to be written to shared memory.
	AtomicOperations() (ids.ID, *atomic.Requests, error)
}

// ProposalTx is an unsigned operation that can be proposed
type ProposalTx interface {
	Tx

	// Attempts to verify this transaction with the provided txstate.
	Execute(verifier TxVerifier, state state.Mutable, creds []verify.Verifiable) (
		onCommitState state.Versioned,
		onAbortState state.Versioned,
		err error,
	)
	InitiallyPrefersCommit(verifier TxVerifier) bool
}

// AtomicTx is an unsigned operation that can be atomically accepted. DEPRECATED
type AtomicTx interface {
	DecisionTx

	// Execute this transaction with the provided txstate.
	AtomicExecute(verifier TxVerifier, parentState state.Mutable, creds []verify.Verifiable) (
		state.Versioned,
		error,
	)

	// Accept this transaction with the additionally provided state transitions.
	AtomicAccept(ctx *snow.Context, batch database.Batch) error
}
