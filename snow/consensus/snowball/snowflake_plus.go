// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

var _ Consensus = (*snowflakePlus)(nil)

// snowflakePlus implements Consensus using Snowflake with multiple
// alpha thresholds and corresponding beta values.
type snowflakePlus struct {
	// alphaPreference is the threshold of votes required to update the preference
	// of the consensus instance.
	alphaPreference int

	// minAlphaConfidence is the lowest alpha threshold that can be used to find
	// the termination point.
	minAlphaConfidence int

	// betas are the number of required successful polls corresponding to
	// each alpha threshold required to finalize.
	// The ith element of betas is the number of consecutive polls that
	// received >= minAlphaConfidence + i votes required to finalize.
	betas []int

	// confidence tracks the number of successful polls in a row for each
	// corresponding alpha threshold.
	// The ith element of confidence is the number of consecutive polls
	// that received >= minAlphaConfidence + i votes.
	confidence []int

	finalized bool

	preference ids.ID
}

func newSnowflakePlus(alphaPreference int, minAlphaConfidence int, betas []int, choice ids.ID) *snowflakePlus {
	return &snowflakePlus{
		alphaPreference:    alphaPreference,
		minAlphaConfidence: minAlphaConfidence,
		betas:              betas,
		confidence:         make([]int, len(betas)),
		preference:         choice,
	}
}

// Add is a no-op for snowflakePlus because it has no notion of beta rogue.
func (*snowflakePlus) Add(ids.ID) {}

// Returns the currently preferred choice to be finalized
func (s *snowflakePlus) Preference() ids.ID {
	return s.preference
}

// RecordPoll records the results of a network poll. Assumes all choices
// have been previously added.
//
// If the consensus instance was not previously finalized, this function
// will return true if the poll was successful and false if the poll was
// unsuccessful.
//
// If the consensus instance was previously finalized, the function may
// return true or false.
func (s *snowflakePlus) RecordPoll(votes bag.Bag[ids.ID]) bool {
	choice, count := votes.Mode()

	// If a new choice received an alphaPreference threshold of votes,
	// update my preference and zero out my confidence counters.
	if choice != s.preference && count >= s.alphaPreference {
		s.preference = choice
		clear(s.confidence)
	}

	// Increment the confidence counter for each alpha threshold in [minAlphaConfidence, count]
	for i := 0; i <= count-s.minAlphaConfidence; i++ {
		s.confidence[i]++
		s.finalized = s.finalized || s.confidence[i] >= s.betas[i]
	}
	// Reset the confidence counter for each alpha threshold in (count, len(confidence))
	for i := count - s.minAlphaConfidence + 1; i < len(s.confidence); i++ {
		s.confidence[i] = 0
	}

	return count >= s.minAlphaConfidence
}

// RecordUnsuccessfulPoll resets the snowflake counters of this consensus
// instance
func (s *snowflakePlus) RecordUnsuccessfulPoll() {
	clear(s.confidence)
}

// Return whether a choice has been finalized
func (s *snowflakePlus) Finalized() bool {
	return s.finalized
}

func (s *snowflakePlus) String() string {
	return fmt.Sprintf(
		"SFP(Choice = %s, Confidence = %v, Finalized = %v)",
		s.preference,
		s.confidence,
		s.finalized,
	)
}
