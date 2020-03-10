// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
)

// binarySnowflake is the implementation of a binary snowflake instance
type binarySnowflake struct {
	// preference is the choice that last had a successful poll. Unless there
	// hasn't been a successful poll, in which case it is the initially provided
	// choice.
	preference int

	// confidence tracks the number of successful polls in a row that have
	// returned the preference
	confidence int

	// beta is the number of consecutive successful queries required for
	// finalization.
	beta int

	// finalized prevents the state from changing after the required number of
	// consecutive polls has been reached
	finalized bool
}

// Initialize implements the BinarySnowflake interface
func (sf *binarySnowflake) Initialize(beta, choice int) {
	sf.beta = beta
	sf.preference = choice
}

// Preference implements the BinarySnowflake interface
func (sf *binarySnowflake) Preference() int { return sf.preference }

// RecordSuccessfulPoll implements the BinarySnowflake interface
func (sf *binarySnowflake) RecordSuccessfulPoll(choice int) {
	if sf.Finalized() {
		return // This instace is already decided.
	}

	if sf.preference == choice {
		sf.confidence++
	} else {
		// confidence is set to 1 because there has already been 1 successful
		// poll, namely this poll.
		sf.confidence = 1
		sf.preference = choice
	}

	sf.finalized = sf.confidence >= sf.beta
}

// RecordUnsuccessfulPoll implements the BinarySnowflake interface
func (sf *binarySnowflake) RecordUnsuccessfulPoll() { sf.confidence = 0 }

// Finalized implements the BinarySnowflake interface
func (sf *binarySnowflake) Finalized() bool { return sf.finalized }

func (sf *binarySnowflake) String() string {
	return fmt.Sprintf("SF(Preference = %d, Confidence = %d, Finalized = %v)",
		sf.Preference(),
		sf.confidence,
		sf.Finalized())
}
