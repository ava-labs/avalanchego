// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/set"

	sbcon "github.com/ava-labs/avalanchego/snow/consensus/snowball"
)

// Consensus is a snowball instance deciding between an unbounded number of
// non-transitive conflicts. After performing a network sample of k nodes, you
// should call collect with the responses.
type Consensus interface {
	fmt.Stringer
	health.Checker

	// Takes in the context, alpha, betaVirtuous, and betaRogue
	Initialize(*snow.ConsensusContext, sbcon.Parameters) error

	// Returns true if transaction <Tx> is virtuous.
	// That is, no transaction has been added that conflicts with <Tx>
	IsVirtuous(Tx) bool

	// Adds a new transaction to vote on. Returns if a critical error has
	// occurred.
	Add(context.Context, Tx) error

	// Remove a transaction from the set of currently processing txs. It is
	// assumed that the provided transaction ID is currently processing.
	Remove(context.Context, ids.ID) error

	// Returns true iff transaction <Tx> has been added
	Issued(Tx) bool

	// Returns the set of virtuous transactions
	// that have not yet been accepted or rejected
	Virtuous() set.Set[ids.ID]

	// Returns the currently preferred transactions to be finalized
	Preferences() set.Set[ids.ID]

	// Return the current virtuous transactions that are being voted on.
	VirtuousVoting() set.Set[ids.ID]

	// Returns the set of transactions conflicting with <Tx>
	Conflicts(Tx) set.Set[ids.ID]

	// Collects the results of a network poll. Assumes all transactions
	// have been previously added. Returns true if any statuses or preferences
	// changed. Returns if a critical error has occurred.
	RecordPoll(context.Context, bag.Bag[ids.ID]) (bool, error)

	// Returns true iff all remaining transactions are rogue. Note, it is
	// possible that after returning quiesce, a new decision may be added such
	// that this instance should no longer quiesce.
	Quiesce() bool

	// Returns true iff all added transactions have been finalized. Note, it is
	// possible that after returning finalized, a new decision may be added such
	// that this instance is no longer finalized.
	Finalized() bool
}
