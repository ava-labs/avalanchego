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
		alphaConfidence: []int{alphaConfidence},
		beta:            []int{beta},
		confidence:      make([]int, 1),
	}
}

// nnarySnowflake is the implementation of a snowflake instance with an
// unbounded number of choices
type nnarySnowflake struct {
	// wrap the n-nary slush logic
	nnarySlush

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

func (*nnarySnowflake) Add(_ ids.ID) {}

func (sf *nnarySnowflake) RecordPoll(count int, choice ids.ID) {
	if sf.finalized {
		return // This instance is already decided.
	}

	if choice != sf.preference && count >= sf.alphaPreference {
		sf.preference = choice
		clear(sf.confidence)
	}

	minAlphaConfidence := sf.alphaConfidence[0]
	if count < minAlphaConfidence {
		sf.RecordUnsuccessfulPoll()
		return
	}

	for i, alphaConfidence := range sf.alphaConfidence {
		if count >= alphaConfidence {
			sf.confidence[i]++
			if sf.confidence[i] >= sf.beta[i] {
				sf.finalized = true
				return
			}
		} else {
			// For all i' >= i, set confidence[i'] = 0
			clear(sf.confidence[i:])
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
	return fmt.Sprintf("SF(Confidence = %d, Finalized = %v, %s)",
		sf.confidence,
		sf.finalized,
		&sf.nnarySlush)
}
