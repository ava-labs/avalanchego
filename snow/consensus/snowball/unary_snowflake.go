// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"
	"slices"
)

var _ Unary = (*unarySnowflake)(nil)

func newUnarySnowflake(alphaPreference int, terminationConditions []terminationCondition) unarySnowflake {
	return unarySnowflake{
		alphaPreference:       alphaPreference,
		terminationConditions: terminationConditions,
		confidence:            make([]int, len(terminationConditions)),
	}
}

// unarySnowflake is the implementation of a unary snowflake instance
// Invariant:
// len(terminationConditions) == len(confidence)
// terminationConditions[i].alphaConfidence < terminationConditions[i+1].alphaConfidence
// terminationConditions[i].beta >= terminationConditions[i+1].beta
// confidence[i] >= confidence[i+1] (except after finalizing due to early termination)
type unarySnowflake struct {
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

func (sf *unarySnowflake) RecordPoll(count int) {
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

func (sf *unarySnowflake) RecordUnsuccessfulPoll() {
	clear(sf.confidence)
}

func (sf *unarySnowflake) Finalized() bool {
	return sf.finalized
}

func (sf *unarySnowflake) Extend(choice int) Binary {
	return &binarySnowflake{
		binarySlush:           binarySlush{preference: choice},
		confidence:            slices.Clone(sf.confidence),
		alphaPreference:       sf.alphaPreference,
		terminationConditions: sf.terminationConditions,
		finalized:             sf.finalized,
	}
}

func (sf *unarySnowflake) Clone() Unary {
	newSnowflake := *sf
	newSnowflake.confidence = slices.Clone(sf.confidence)
	return &newSnowflake
}

func (sf *unarySnowflake) String() string {
	return fmt.Sprintf("SF(Confidence = %v, Finalized = %v)",
		sf.confidence,
		sf.finalized)
}
