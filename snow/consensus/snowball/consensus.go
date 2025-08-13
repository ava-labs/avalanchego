// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

// Consensus represents a general snow instance that can be used directly to
// process the results of network queries.
type Consensus interface {
	fmt.Stringer

	// Adds a new choice to vote on
	Add(newChoice ids.ID)

	// Returns the currently preferred choice to be finalized
	Preference() ids.ID

	// RecordPoll records the results of a network poll. Assumes all choices
	// have been previously added.
	//
	// If the consensus instance was not previously finalized, this function
	// will return true if the poll was successful and false if the poll was
	// unsuccessful.
	//
	// If the consensus instance was previously finalized, the function may
	// return true or false.
	RecordPoll(votes bag.Bag[ids.ID]) bool

	// RecordUnsuccessfulPoll resets the snowflake counters of this consensus
	// instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool
}

// Factory produces Nnary and Unary decision instances
type Factory interface {
	NewNnary(params Parameters, choice ids.ID) Nnary
	NewUnary(params Parameters) Unary
}

// Nnary is a snow instance deciding between an unbounded number of values.
// The caller samples k nodes and calls RecordPoll with the result.
// RecordUnsuccessfulPoll resets the confidence counters when one or
// more consecutive polls fail to reach alphaPreference votes.
type Nnary interface {
	fmt.Stringer

	// Adds a new possible choice
	Add(newChoice ids.ID)

	// Returns the currently preferred choice to be finalized
	Preference() ids.ID

	// RecordPoll records the results of a network poll
	RecordPoll(count int, choice ids.ID)

	// RecordUnsuccessfulPoll resets the snowflake counter of this instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool
}

// Binary is a snow instance deciding between two values.
// The caller samples k nodes and calls RecordPoll with the result.
// RecordUnsuccessfulPoll resets the confidence counters when one or
// more consecutive polls fail to reach alphaPreference votes.
type Binary interface {
	fmt.Stringer

	// Returns the currently preferred choice to be finalized
	Preference() int

	// RecordPoll records the results of a network poll
	RecordPoll(count, choice int)

	// RecordUnsuccessfulPoll resets the snowflake counter of this instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool
}

// Unary is a snow instance deciding on one value.
// The caller samples k nodes and calls RecordPoll with the result.
// RecordUnsuccessfulPoll resets the confidence counters when one or
// more consecutive polls fail to reach alphaPreference votes.
type Unary interface {
	fmt.Stringer

	// RecordPoll records the results of a network poll
	RecordPoll(count int)

	// RecordUnsuccessfulPoll resets the snowflake counter of this instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool

	// Returns a new binary snowball instance with the original choice.
	Extend(originalPreference int) Binary

	// Returns a new unary snowflake instance with the same state
	Clone() Unary
}
