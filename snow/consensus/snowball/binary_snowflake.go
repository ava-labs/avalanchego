// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Binary = (*binarySnowflake)(nil)

func newBinarySnowflake(alphaPreference int, alphaConfidence []int, beta []int, choice int) binarySnowflake {
	return binarySnowflake{
		binarySlush:     newBinarySlush(choice),
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
		beta:            beta,
		confidence:      make([]int, len(alphaConfidence)),
	}
}

// binarySnowflake is the implementation of a binary snowflake instance
// Invariant:
// len(alphaConfidence) == len(beta) == len(confidence)
// alphaConfidence[i] < alphaConfidence[i+1]
// beta[i] < beta[i+1]
// confidence[i] >= confidence[i+1] (except after finalizing due to early termination)
type binarySnowflake struct {
	// wrap the binary slush logic
	binarySlush

	// alphaPreference is the threshold required to update the preference
	alphaPreference int

	// alphaConfidence[i] gives the alphaConfidence threshold required to increment
	// confidence[i]
	alphaConfidence []int

	// beta[i] gives the number of consecutive polls reaching alphaConfidence[i]
	// required to finalize.
	beta []int

	// confidence is the number of consecutive succcessful polls for a given
	// alphaConfidence threshold.
	// This instance finalizes when confidence[i] >= beta[i] for any i
	confidence []int

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

	// If I need to change my preference, record the new preference
	// and reset all my confidence counters.
	if choice != sf.Preference() {
		sf.binarySlush.RecordSuccessfulPoll(choice)
		clear(sf.confidence)
	}

	for i, alphaConfidence := range sf.alphaConfidence {
		// If I did not reach this alpha threshold, I did not
		// reach any more alpha thresholds.
		// Clear the remaining confidence counters.
		if count < alphaConfidence {
			clear(sf.confidence[i:])
			return
		}

		// I reached this alpha threshold, increment the confidence counter
		// and check if I can finalize.
		sf.confidence[i]++
		if sf.confidence[i] >= sf.beta[i] {
			sf.finalized = true
			return
		}
	}
}

func (sf *binarySnowflake) RecordUnsuccessfulPoll() {
	clear(sf.confidence)
}

func (sf *binarySnowflake) Finalized() bool {
	return sf.finalized
}

func (sf *binarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %d, Finalized = %v, %s)",
		sf.confidence[0],
		sf.finalized,
		&sf.binarySlush)
}
