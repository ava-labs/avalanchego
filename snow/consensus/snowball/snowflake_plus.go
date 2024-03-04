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
	// alpha1 is the threshold of votes required to update the preference
	// of the consensus instance.
	alpha1 int

	// alpha2Cutoff is the lowest alpha threshold that can be used to find
	// the termination point.
	alpha2Cutoff int

	// betas are the number of required successful polls corresponding to
	// each alpha threshold required to finalize.
	// The ith element of betas is the number of consecutive polls that
	// received >= alpha2Cutoff + i votes required to finalize.
	betas []int

	// confidence tracks the number of successful polls in a row for each
	// corresponding alpha threshold.
	// The ith element of confidence is the number of consecutive polls
	// that received >= alpha2Cutoff + i votes.
	confidence []int

	finalized bool

	choice ids.ID
}

func newSnowflakePlus(alpha1 int, alpha2Cutoff int, betas []int, choice ids.ID) *snowflakePlus {
	return &snowflakePlus{
		alpha1:       alpha1,
		alpha2Cutoff: alpha2Cutoff,
		betas:        betas,
		confidence:   make([]int, len(betas)),
		choice:       choice,
	}
}

// Add is a no-op for snowflakePlus because it has no notion of beta rogue.
func (s *snowflakePlus) Add(ids.ID) {}

// Returns the currently preferred choice to be finalized
func (s *snowflakePlus) Preference() ids.ID {
	return s.choice
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

	// If a new choice received an alpha1 threshold of votes,
	// update my preference and zero out my confidence counters.
	if choice != s.choice && count >= s.alpha1 {
		s.choice = choice
		s.RecordUnsuccessfulPoll()
	}

	// If my choice received an alpha2 threshold, increment the
	// corresponding confidence counter. Otherwise, zero out
	// the confidence counter.
	for i := 0; i < len(s.confidence); i++ {
		if count >= s.alpha2Cutoff+i {
			s.confidence[i]++
			if s.confidence[i] >= s.betas[i] {
				s.finalized = true
			}
		} else {
			s.confidence[i] = 0
		}
	}
	return s.finalized
}

// RecordUnsuccessfulPoll resets the snowflake counters of this consensus
// instance
func (s *snowflakePlus) RecordUnsuccessfulPoll() {
	for i := 0; i < len(s.confidence); i++ {
		s.confidence[i] = 0
	}
}

// Return whether a choice has been finalized
func (s *snowflakePlus) Finalized() bool {
	return s.finalized
}

func (s *snowflakePlus) String() string {
	return fmt.Sprintf(
		"SFP(Choice = %s, Confidence = %v, Finalized = %v)",
		s.choice,
		s.confidence,
		s.finalized,
	)
}
