// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions"
)

// UnsignedDecisionTx is an unsigned operation that can be immediately decided
type UnsignedDecisionTx interface {
	transactions.UnsignedTx

	// Attempts to verify this transactions.with the provided state.
	SemanticVerify(vm *VM, vs VersionedState, stx *transactions.SignedTx) (
		onAcceptFunc func() error,
		err TxError,
	)
}

// UnsignedProposalTx is an unsigned operation that can be proposed
type UnsignedProposalTx interface {
	transactions.UnsignedTx

	// Attempts to verify this transactions.with the provided state.
	SemanticVerify(vm *VM, state MutableState, stx *transactions.SignedTx) (
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
	transactions.UnsignedTx

	// UTXOs this tx consumes
	InputUTXOs() ids.Set
	// Attempts to verify this transactions.with the provided state.
	SemanticVerify(vm *VM, parentState MutableState, stx *transactions.SignedTx) (VersionedState, TxError)

	// Accept this transactions.with the additionally provided state transitions.
	Accept(ctx *snow.Context, batch database.Batch) error
}
