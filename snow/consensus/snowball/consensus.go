// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

// NnarySnowball augments NnarySnowflake with a counter that tracks the total
// number of positive responses from a network sample.
type NnarySnowball interface{ NnarySnowflake }

// NnarySnowflake is a snowflake instance deciding between an unbounded number
// of values. After performing a network sample of k nodes, if you have alpha
// votes for one of the choices, you should vote for that choice. Otherwise, you
// should reset.
type NnarySnowflake interface {
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

// NnarySlush is a slush instance deciding between an unbounded number of
// values. After performing a network sample of k nodes, if you have alpha
// votes for one of the choices, you should vote for that choice.
type NnarySlush interface {
	fmt.Stringer

	// Returns the currently preferred choice to be finalized
	Preference() ids.ID

	// RecordSuccessfulPoll records a successful poll towards finalizing the
	// specified choice. Assumes the choice was previously added.
	RecordSuccessfulPoll(choice ids.ID)
}

// BinarySnowball augments BinarySnowflake with a counter that tracks the total
// number of positive responses from a network sample.
type BinarySnowball interface{ BinarySnowflake }

// BinarySnowflake is a snowball instance deciding between two values
// After performing a network sample of k nodes, if you have alpha votes for
// one of the choices, you should vote for that choice. Otherwise, you should
// reset.
type BinarySnowflake interface {
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

// BinarySlush is a slush instance deciding between two values. After performing
// a network sample of k nodes, if you have alpha votes for one of the choices,
// you should vote for that choice.
type BinarySlush interface {
	fmt.Stringer

	// Returns the currently preferred choice to be finalized
	Preference() int

	// RecordSuccessfulPoll records a successful poll towards finalizing the
	// specified choice
	RecordSuccessfulPoll(choice int)
}

// UnarySnowball is a snowball instance deciding on one value. After performing
// a network sample of k nodes, if you have alpha votes for the choice, you
// should vote. Otherwise, you should reset.
type UnarySnowball interface {
	fmt.Stringer

	// RecordSuccessfulPoll records a successful poll towards finalizing
	RecordSuccessfulPoll()

	// RecordPollPreference records a poll that strengthens the preference but
	// did not contribute towards finalizing
	RecordPollPreference()

	// RecordUnsuccessfulPoll resets the snowflake counter of this instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool

	// Returns a new binary snowball instance with the agreement parameters
	// transferred. Takes in the new beta value and the original choice
	Extend(beta, originalPreference int) BinarySnowball

	// Returns a new unary snowball instance with the same state
	Clone() UnarySnowball
}

// UnarySnowflake is a snowflake instance deciding on one value. After
// performing a network sample of k nodes, if you have alpha votes for the
// choice, you should vote. Otherwise, you should reset.
type UnarySnowflake interface {
	fmt.Stringer

	// RecordSuccessfulPoll records a successful poll towards finalizing
	RecordSuccessfulPoll()

	// RecordUnsuccessfulPoll resets the snowflake counter of this instance
	RecordUnsuccessfulPoll()

	// Return whether a choice has been finalized
	Finalized() bool

	// Returns a new binary snowball instance with the agreement parameters
	// transferred. Takes in the new beta value and the original choice
	Extend(beta, originalPreference int) BinarySnowflake

	// Returns a new unary snowflake instance with the same state
	Clone() UnarySnowflake
}
