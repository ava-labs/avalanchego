// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Binary = (*binarySnowflake)(nil)

func newBinarySnowflake(alphaPreference int, terminationConditions []terminationCondition, choice int) binarySnowflake {
	return binarySnowflake{
		binarySlush:           newBinarySlush(choice),
		alphaPreference:       alphaPreference,
		terminationConditions: terminationConditions,
		confidence:            make([]int, len(terminationConditions)),
	}
}

// binarySnowflake is the implementation of a binary snowflake instance
// Invariant:
// len(terminationConditions) == len(confidence)
// terminationConditions[i].alphaConfidence < terminationConditions[i+1].alphaConfidence
// terminationConditions[i].beta >= terminationConditions[i+1].beta
// confidence[i] >= confidence[i+1] (except after finalizing due to early termination)
type binarySnowflake struct {
	// wrap the binary slush logic
	binarySlush

	// alphaPreference is the threshold required to update the preference
	alphaPreference int

	// terminationConditions gives the ascending ordered list of alphaConfidence values
	// required to increment the corresponding confidence counter.
	// The corresponding beta values give the threshold required to finalize this instance.
	terminationConditions []terminationCondition

	// confidence is the number of consecutive successful polls for a given
	// alphaConfidence threshold.
	// This instance finalizes when confidence[i] >= terminationConditions[i].beta for any i
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

	// If I am changing my preference, reset confidence counters
	// before recording a successful poll on the slush instance.
	if choice != sf.Preference() {
		clear(sf.confidence)
	}
	sf.binarySlush.RecordSuccessfulPoll(choice)

	for i, terminationCondition := range sf.terminationConditions {
		// If I did not reach this alpha threshold, I did not
		// reach any more alpha thresholds.
		// Clear the remaining confidence counters.
		if count < terminationCondition.alphaConfidence {
			clear(sf.confidence[i:])
			return
		}

		// I reached this alpha threshold, increment the confidence counter
		// and check if I can finalize.
		sf.confidence[i]++
		if sf.confidence[i] >= terminationCondition.beta {
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
	return fmt.Sprintf("SF(Confidence = %v, Finalized = %v, %s)",
		sf.confidence,
		sf.finalized,
		&sf.binarySlush)
}
