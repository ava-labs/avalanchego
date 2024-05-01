// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Unary = (*unarySnowflake)(nil)

func newUnarySnowflake(alphaPreference, alphaConfidence, beta int) unarySnowflake {
	return unarySnowflake{
		alphaPreference: alphaPreference,
		alphaConfidence: []int{alphaConfidence},
		beta:            []int{beta},
		confidence:      make([]int, 1),
	}
}

// unarySnowflake is the implementation of a unary snowflake instance
// Invariant:
// len(alphaConfidence) == len(beta) == len(confidence)
// alphaConfidence[i] < alphaConfidence[i+1]
// beta[i] < beta[i+1]
// confidence[i] >= confidence[i+1] (except after finalizing due to early termination)
type unarySnowflake struct {
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

func (sf *unarySnowflake) RecordPoll(count int) {
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

func (sf *unarySnowflake) RecordUnsuccessfulPoll() {
	clear(sf.confidence)
}

func (sf *unarySnowflake) Finalized() bool {
	return sf.finalized
}

func (sf *unarySnowflake) Extend(choice int) Binary {
	confidence := make([]int, len(sf.confidence))
	copy(confidence, sf.confidence)
	return &binarySnowflake{
		binarySlush:     binarySlush{preference: choice},
		confidence:      confidence,
		alphaPreference: sf.alphaPreference,
		alphaConfidence: sf.alphaConfidence,
		beta:            sf.beta,
		finalized:       sf.finalized,
	}
}

func (sf *unarySnowflake) Clone() Unary {
	newSnowflake := *sf
	// Copy the confidence slice
	newSnowflake.confidence = make([]int, len(sf.confidence))
	copy(newSnowflake.confidence, sf.confidence)
	return &newSnowflake
}

func (sf *unarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %d, Finalized = %v)",
		sf.confidence[0],
		sf.finalized)
}
