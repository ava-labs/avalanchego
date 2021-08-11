// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/transaction"
)

// UnsignedDecisionTx is an unsigned operation that can be immediately decided
type UnsignedDecisionTx interface {
	transaction.UnsignedTx

	// Attempts to verify this transaction with the provided state.
	SemanticVerify(vm *VM, vs VersionedState, stx *transaction.SignedTx) (
		onAcceptFunc func() error,
		err TxError,
	)
}

// UnsignedProposalTx is an unsigned operation that can be proposed
type UnsignedProposalTx interface {
	transaction.UnsignedTx

	// Attempts to verify this transaction with the provided state.
	SemanticVerify(vm *VM, state MutableState, stx *transaction.SignedTx) (
		onCommitState VersionedState,
		onAbortState VersionedState,
		onCommitFunc func() error,
		onAbortFunc func() error,
		err TxError,
	)
	InitiallyPrefersCommit(vm *VM) bool
}

// UnsignedAtomicTx is an unsigned operation that can be atomically accepted
type UnsignedAtomicTx interface {
	transaction.UnsignedTx

	// UTXOs this tx consumes
	InputUTXOs() ids.Set
	// Attempts to verify this transaction with the provided state.
	SemanticVerify(vm *VM, parentState MutableState, stx *transaction.SignedTx) (VersionedState, TxError)

	// Accept this transaction with the additionally provided state transitions.
	Accept(ctx *snow.Context, batch database.Batch) error
}
