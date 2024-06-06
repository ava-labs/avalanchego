// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Binary = (*binarySnowflake)(nil)

func newBinarySnowflake(alphaPreference, alphaConfidence, beta, choice int) binarySnowflake {
	return binarySnowflake{
		binarySlush:     newBinarySlush(choice),
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
		beta:            beta,
	}
}

// binarySnowflake is the implementation of a binary snowflake instance
type binarySnowflake struct {
	// wrap the binary slush logic
	binarySlush

	// confidence tracks the number of successful polls in a row that have
	// returned the preference
	confidence int

	// alphaPreference is the threshold required to update the preference
	alphaPreference int

	// alphaConfidence is the threshold required to increment the confidence counter
	alphaConfidence int

	// beta is the number of consecutive successful queries required for
	// finalization.
	beta int

	// finalized prevents the state from changing after the required number of
	// consecutive polls has been reached
	finalized bool
}

func (sf *binarySnowflake) RecordPoll(count, choice int) {
	if sf.finalized {
		return // This instance is already decided.
	}

	if count < sf.alphaPreference {
		sf.RecordUnsuccessfulPoll()
		return
	}

	if count < sf.alphaConfidence {
		sf.confidence = 0
		sf.binarySlush.RecordSuccessfulPoll(choice)
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
	sf.binarySlush.RecordSuccessfulPoll(choice)
}

func (sf *binarySnowflake) RecordUnsuccessfulPoll() {
	sf.confidence = 0
}

func (sf *binarySnowflake) Finalized() bool {
	return sf.finalized
}

func (sf *binarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %d, Finalized = %v, %s)",
		sf.confidence,
		sf.finalized,
		&sf.binarySlush)
}
