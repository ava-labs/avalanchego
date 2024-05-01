// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var _ Nnary = (*nnarySnowflake)(nil)

func newNnarySnowflake(alphaPreference int, alphaConfidence []int, beta []int, choice ids.ID) nnarySnowflake {
	return nnarySnowflake{
		nnarySlush:      newNnarySlush(choice),
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
		beta:            beta,
		confidence:      make([]int, len(alphaConfidence)),
	}
}

// nnarySnowflake is the implementation of a snowflake instance with an
// unbounded number of choices
// Invariant:
// len(alphaConfidence) == len(beta) == len(confidence)
// alphaConfidence[i] < alphaConfidence[i+1]
// beta[i] < beta[i+1]
// confidence[i] >= confidence[i+1] (except after finalizing due to early termination)
type nnarySnowflake struct {
	// wrap the n-nary slush logic
	nnarySlush

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

func (*nnarySnowflake) Add(_ ids.ID) {}

func (sf *nnarySnowflake) RecordPoll(count int, choice ids.ID) {
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
		clear(sf.confidence)
		sf.nnarySlush.RecordSuccessfulPoll(choice)
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

func (sf *nnarySnowflake) RecordUnsuccessfulPoll() {
	clear(sf.confidence)
}

func (sf *nnarySnowflake) Finalized() bool {
	return sf.finalized
}

func (sf *nnarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %v, Finalized = %v, %s)",
		sf.confidence,
		sf.finalized,
		&sf.nnarySlush)
}
