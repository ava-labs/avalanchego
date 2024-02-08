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

// ConsensusFactory produces NnarySnowflake instances
type ConsensusFactory interface {
	New(params Parameters, choice ids.ID) NnarySnow
	NewUnary(params Parameters) UnarySnow
}

// NnarySnow is a snow instance deciding between an unbounded number
// of values.
// After the caller performs a sample of k nodes, it will call
// 1. RecordSuccessfulPoll if choice collects >= alphaConfidence votes
// 2. RecordPollPreference if choice collects >= alphaPreference votes
// 3. RecordUnsuccessfulPoll otherwise
type NnarySnow interface {
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

// BinarySnow is a snow instance deciding between two values.
// After the caller performs a sample of k nodes, it will call
// 1. RecordSuccessfulPoll if choice collects >= alphaConfidence votes
// 2. RecordPollPreference if choice collects >= alphaPreference votes
// 3. RecordUnsuccessfulPoll otherwise
type BinarySnow interface {
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

// UnarySnow is a snow instance deciding on one value. After
// After the caller performs a sample of k nodes, it will call
// 1. RecordSuccessfulPoll if choice collects >= alphaConfidence votes
// 2. RecordPollPreference if choice collects >= alphaPreference votes
// 3. RecordUnsuccessfulPoll otherwise
type UnarySnow interface {
	fmt.Stringer

	// RecordSuccessfulPoll records a successful poll towards finalizing
	RecordSuccessfulPoll()

	// RecordPollPreference records a poll that strengthens the preference but
	// did not contribute towards finalizing. This is a no-op for UnarySnowflake
	// because there is only one choice, so it's preference will not change, there
	// is no preference confidence to increment, and it does not increment confidence
	// towards accept. TODO: improve comment
	RecordPollPreference()

	// RecordUnsuccessfulPoll resets the snowflake counter of this instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool

	// Returns a new binary snowball instance with the agreement parameters
	// transferred. Takes in the new beta value and the original choice
	Extend(beta, originalPreference int) BinarySnow

	// Returns a new unary snowflake instance with the same state
	Clone() UnarySnow
}
