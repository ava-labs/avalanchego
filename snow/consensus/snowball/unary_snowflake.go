// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import "fmt"

var _ Unary = (*unarySnowflake)(nil)

func newUnarySnowflake(alphaPreference, alphaConfidence, beta int) unarySnowflake {
	return unarySnowflake{
		alphaPreference: alphaPreference,
		alphaConfidence: []int{alphaConfidence},
		confidence:      make([]int, 1),
		beta:            []int{beta},
	}
}

// unarySnowflake is the implementation of a unary snowflake instance
type unarySnowflake struct {
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

func (sf *unarySnowflake) RecordPoll(count int) {
	if sf.finalized {
		return // This instance is already decided.
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

func (sf *unarySnowflake) RecordUnsuccessfulPoll() {
	clear(sf.confidence)
}

func (sf *unarySnowflake) Finalized() bool {
	return sf.finalized
}

func (sf *unarySnowflake) Extend(choice int) Binary {
	return &binarySnowflake{
		binarySlush:     binarySlush{preference: choice},
		confidence:      sf.confidence,
		alphaPreference: sf.alphaPreference,
		alphaConfidence: sf.alphaConfidence,
		beta:            sf.beta,
		finalized:       sf.finalized,
	}
}

func (sf *unarySnowflake) Clone() Unary {
	newSnowflake := *sf
	return &newSnowflake
}

func (sf *unarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %d, Finalized = %v)",
		sf.confidence,
		sf.finalized)
}
