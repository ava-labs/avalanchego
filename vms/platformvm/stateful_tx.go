// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

// StatefulTx is an unsigned transaction
type StatefulTx interface {
	unsigned.Tx

	// Attempts to verify this transaction with the provided state.
	SemanticVerify(vm *VM, parentState MutableState, stx *signed.Tx) error
}

// StatefulDecisionTx is an unsigned operation that can be immediately decided
type StatefulDecisionTx interface {
	StatefulTx

	// Execute this transaction with the provided state.
	Execute(vm *VM, vs VersionedState, stx *signed.Tx) (
		onAcceptFunc func() error,
		err error,
	)

	// To maintain consistency with the Atomic txs
	InputUTXOs() ids.Set

	// AtomicOperations provides the requests to be written to shared memory.
	AtomicOperations() (ids.ID, *atomic.Requests, error)
}

// StatefulProposalTx is an unsigned operation that can be proposed
type StatefulProposalTx interface {
	StatefulTx

	// Attempts to verify this transaction with the provided state.
	Execute(vm *VM, state MutableState, stx *signed.Tx) (
		onCommitState VersionedState,
		onAbortState VersionedState,
		err error,
	)
	InitiallyPrefersCommit(vm *VM) bool
}

// StatefulAtomicTx is an unsigned operation that can be atomically accepted
type StatefulAtomicTx interface {
	StatefulDecisionTx

	// Execute this transaction with the provided state.
	AtomicExecute(vm *VM, parentState MutableState, stx *signed.Tx) (VersionedState, error)

	// Accept this transaction with the additionally provided state transitions.
	AtomicAccept(ctx *snow.Context, batch database.Batch) error
}
