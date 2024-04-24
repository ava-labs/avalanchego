// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Binary = (*binarySnowflake)(nil)

func newBinarySnowflake(alphaPreference int, alphaConfidence int, beta int, choice int) binarySnowflake {
	return binarySnowflake{
		binarySlush:     newBinarySlush(choice),
		alphaPreference: alphaPreference,
		alphaConfidence: []int{alphaConfidence},
		beta:            []int{beta},
		confidence:      make([]int, 1),
	}
}

// binarySnowflake is the implementation of a binary snowflake instance
type binarySnowflake struct {
	// wrap the binary slush logic
	binarySlush

	// alphaPreference is the threshold required to update the preference
	alphaPreference int

	// alphaConfidence[i] gives the alphaConfidence threshold required to increment
	// confidence[i]
	alphaConfidence []int

	// beta[i] gives the number of consecutive successful polls required to finalize
	// after reaching an
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

	if choice != sf.preference && count >= sf.alphaPreference {
		sf.binarySlush.RecordSuccessfulPoll(choice)
		clear(sf.confidence)
	}

	minAlphaConfidence := sf.alphaConfidence[0]
	if count < minAlphaConfidence {
		sf.RecordUnsuccessfulPoll()
		return
	}

	for i, alphaConfidence := range sf.alphaConfidence {
		if count < alphaConfidence {
			clear(sf.confidence[i:])
			return
		}

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
