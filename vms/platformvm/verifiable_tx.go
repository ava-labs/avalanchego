// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions"
)

// VerifiableUnsignedDecisionTx is an unsigned operation that can be immediately decided
type VerifiableUnsignedDecisionTx interface {
	transactions.UnsignedDecisionTx

	// Attempts to verify this transactions.with the provided state.
	SemanticVerify(vm *VM, vs VersionedState, stx *transactions.SignedTx) (
		onAcceptFunc func() error,
		err TxError,
	)
}

// VerifiableUnsignedProposalTx is an unsigned operation that can be proposed
type VerifiableUnsignedProposalTx interface {
	transactions.UnsignedProposalTx

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

// VerifiableUnsignedAtomicTx is an unsigned operation that can be atomically accepted
type VerifiableUnsignedAtomicTx interface {
	transactions.UnsignedAtomicTx

	// UTXOs this tx consumes
	InputUTXOs() ids.Set
	// Attempts to verify this transactions.with the provided state.
	SemanticVerify(vm *VM, parentState MutableState, stx *transactions.SignedTx) (VersionedState, TxError)

	// Accept this transactions.with the additionally provided state transitions.
	Accept(ctx *snow.Context, batch database.Batch) error
}
