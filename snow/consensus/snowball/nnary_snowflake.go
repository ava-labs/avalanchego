// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

// nnarySnowflake is the implementation of a snowflake instance with an
// unbounded number of choices
type nnarySnowflake struct {
	// wrap the n-nary slush logic
	nnarySlush

	// betaVirtuous is the number of consecutive successful queries required for
	// finalization on a virtuous instance.
	betaVirtuous int

	// betaRogue is the number of consecutive successful queries required for
	// finalization on a rogue instance.
	betaRogue int

	// confidence tracks the number of successful polls in a row that have
	// returned the preference
	confidence int

	// rogue tracks if this instance has multiple choices or only one
	rogue bool

	// finalized prevents the state from changing after the required number of
	// consecutive polls has been reached
	finalized bool
}

// Initialize implements the NnarySnowflake interface
func (sf *nnarySnowflake) Initialize(betaVirtuous, betaRogue int, choice ids.ID) {
	sf.nnarySlush.Initialize(choice)
	sf.betaVirtuous = betaVirtuous
	sf.betaRogue = betaRogue
}

// Add implements the NnarySnowflake interface
func (sf *nnarySnowflake) Add(choice ids.ID) { sf.rogue = sf.rogue || choice != sf.preference }

// RecordSuccessfulPoll implements the NnarySnowflake interface
func (sf *nnarySnowflake) RecordSuccessfulPoll(choice ids.ID) {
	if sf.finalized {
		return // This instace is already decided.
	}

	if preference := sf.Preference(); preference == choice {
		sf.confidence++
	} else {
		// confidence is set to 1 because there has already been 1 successful
		// poll, namely this poll.
		sf.confidence = 1
	}

	sf.finalized = (!sf.rogue && sf.confidence >= sf.betaVirtuous) ||
		sf.confidence >= sf.betaRogue
	sf.nnarySlush.RecordSuccessfulPoll(choice)
}

// RecordUnsuccessfulPoll implements the NnarySnowflake interface
func (sf *nnarySnowflake) RecordUnsuccessfulPoll() { sf.confidence = 0 }

// Finalized implements the NnarySnowflake interface
func (sf *nnarySnowflake) Finalized() bool { return sf.finalized }

func (sf *nnarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %d, Finalized = %v, %s)",
		sf.confidence,
		sf.finalized,
		&sf.nnarySlush)
}
