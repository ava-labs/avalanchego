// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Nnary = (*nnarySnowflake)(nil)

func newNnarySnowflake(alphaPreference, alphaConfidence, beta int, choice ids.ID) nnarySnowflake {
	return nnarySnowflake{
		nnarySlush:      newNnarySlush(choice),
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
		beta:            beta,
	}
}

// nnarySnowflake is the implementation of a snowflake instance with an
// unbounded number of choices
type nnarySnowflake struct {
	// wrap the n-nary slush logic
	nnarySlush

	// beta is the number of consecutive successful queries required for
	// finalization.
	beta int

	// alphaPreference is the threshold required to update the preference
	alphaPreference int

	// alphaConfidence is the threshold required to increment the confidence counter
	alphaConfidence int

	// confidence tracks the number of successful polls in a row that have
	// returned the preference
	confidence int

	// finalized prevents the state from changing after the required number of
	// consecutive polls has been reached
	finalized bool
}

func (*nnarySnowflake) Add(_ ids.ID) {}

func (sf *nnarySnowflake) RecordPoll(count int, choice ids.ID) {
	if sf.finalized {
		return // This instance is already decided.
	}

	if count < sf.alphaPreference {
		sf.RecordUnsuccessfulPoll()
		return
	}

	if count < sf.alphaConfidence {
		sf.confidence = 0
		sf.nnarySlush.RecordSuccessfulPoll(choice)
		return
	}

	if preference := sf.Preference(); preference == choice {
		sf.confidence++
	} else {
		// confidence is set to 1 because there has already been 1 successful
		// poll, namely this poll.
		sf.confidence = 1
	}

	sf.finalized = sf.confidence >= sf.beta
	sf.nnarySlush.RecordSuccessfulPoll(choice)
}

func (sf *nnarySnowflake) RecordUnsuccessfulPoll() {
	sf.confidence = 0
}

func (sf *nnarySnowflake) Finalized() bool {
	return sf.finalized
}

func (sf *nnarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %d, Finalized = %v, %s)",
		sf.confidence,
		sf.finalized,
		&sf.nnarySlush)
}
