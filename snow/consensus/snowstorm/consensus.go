// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
)

// Consensus is a snowball instance deciding between an unbounded number of
// non-transitive conflicts. After performing a network sample of k nodes, you
// should call collect with the responses.
type Consensus interface {
	fmt.Stringer

	// Takes in the context, alpha, betaVirtuous, and betaRogue
	Initialize(*snow.Context, snowball.Parameters)

	// Returns the parameters that describe this snowstorm instance
	Parameters() snowball.Parameters

	// Returns true if transaction <Tx> is virtuous.
	// That is, no transaction has been added that conflicts with <Tx>
	IsVirtuous(Tx) bool

	// Adds a new transaction to vote on. Returns if a critical error has
	// occurred.
	Add(Tx) error

	// Returns true iff transaction <Tx> has been added
	Issued(Tx) bool

	// Returns the set of virtuous transactions
	// that have not yet been accepted or rejected
	Virtuous() ids.Set

	// Returns the currently preferred transactions to be finalized
	Preferences() ids.Set

	// Returns the set of transactions conflicting with <Tx>
	Conflicts(Tx) ids.Set

	// Collects the results of a network poll. Assumes all transactions
	// have been previously added. Returns if a critical error has occurred.
	RecordPoll(ids.Bag) error

	// Returns true iff all remaining transactions are rogue. Note, it is
	// possible that after returning quiesce, a new decision may be added such
	// that this instance should no longer quiesce.
	Quiesce() bool

	// Returns true iff all added transactions have been finalized. Note, it is
	// possible that after returning finalized, a new decision may be added such
	// that this instance is no longer finalized.
	Finalized() bool
}

// Tx consumes state.
type Tx interface {
	choices.Decidable

	// Dependencies is a list of transactions upon which this transaction
	// depends. Each element of Dependencies must be verified before Verify is
	// called on this transaction.
	//
	// Similarly, each element of Dependencies must be accepted before this
	// transaction is accepted.
	Dependencies() []Tx

	// InputIDs is a set where each element is the ID of a piece of state that
	// will be consumed if this transaction is accepted.
	//
	// In the context of a UTXO-based payments system, for example, this would
	// be the IDs of the UTXOs consumed by this transaction
	InputIDs() ids.Set

	// Verify that the state transition this transaction would make if it were
	// accepted is valid. If the state transition is invalid, a non-nil error
	// should be returned.
	//
	// It is guaranteed that when Verify is called, all the dependencies of
	// this transaction have already been successfully verified.
	Verify() error

	// Bytes returns the binary representation of this transaction.
	//
	// This is used for sending transactions to peers. Another node should be
	// able to parse these bytes to the same transaction.
	Bytes() []byte
}
