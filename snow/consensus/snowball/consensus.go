// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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
// The caller samples k nodes and then calls
// 1. RecordSuccessfulPoll if choice collects >= alphaConfidence votes
// 2. RecordPollPreference if choice collects >= alphaPreference votes
// 3. RecordUnsuccessfulPoll otherwise
type Nnary interface {
	fmt.Stringer

	// Adds a new possible choice
	Add(newChoice ids.ID)

	// Returns the currently preferred choice to be finalized
	Preference() ids.ID

	// RecordSuccessfulPoll records a successful poll towards finalizing the
	// specified choice. Assumes the choice was previously added.
	RecordSuccessfulPoll(choice ids.ID)

	// RecordPollPreference records a poll that preferred the specified choice
	// but did not contribute towards finalizing the specified choice. Assumes
	// the choice was previously added.
	RecordPollPreference(choice ids.ID)

	// RecordUnsuccessfulPoll resets the snowflake counter of this instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool
}

// Binary is a snow instance deciding between two values.
// The caller samples k nodes and then calls
// 1. RecordSuccessfulPoll if choice collects >= alphaConfidence votes
// 2. RecordPollPreference if choice collects >= alphaPreference votes
// 3. RecordUnsuccessfulPoll otherwise
type Binary interface {
	fmt.Stringer

	// Returns the currently preferred choice to be finalized
	Preference() int

	// RecordSuccessfulPoll records a successful poll towards finalizing the
	// specified choice
	RecordSuccessfulPoll(choice int)

	// RecordPollPreference records a poll that preferred the specified choice
	// but did not contribute towards finalizing the specified choice
	RecordPollPreference(choice int)

	// RecordUnsuccessfulPoll resets the snowflake counter of this instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool
}

// Unary is a snow instance deciding on one value.
// The caller samples k nodes and then calls
// 1. RecordSuccessfulPoll if choice collects >= alphaConfidence votes
// 2. RecordPollPreference if choice collects >= alphaPreference votes
// 3. RecordUnsuccessfulPoll otherwise
type Unary interface {
	fmt.Stringer

	// RecordSuccessfulPoll records a successful poll that reaches an alpha
	// confidence threshold.
	RecordSuccessfulPoll()

	// RecordPollPreference records a poll that receives an alpha preference
	// threshold, but not an alpha confidence threshold.
	RecordPollPreference()

	// RecordUnsuccessfulPoll resets the snowflake counter of this instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool

	// Returns a new binary snowball instance with the agreement parameters
	// transferred. Takes in the new beta value and the original choice
	Extend(beta, originalPreference int) Binary

	// Returns a new unary snowflake instance with the same state
	Clone() Unary
}
