// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

var _ Consensus = (*Flat)(nil)

func NewFlat(factory ConsensusFactory, params Parameters, choice ids.ID) Consensus {
	sf := factory.New(params, choice)

	return &Flat{
		NnarySnow: sf,
		params:    params,
	}
}

// Flat is a naive implementation of a multi-choice snow instance
type Flat struct {
	// wraps the n-nary snowball logic
	NnarySnow

	// params contains all the configurations of a snowball instance
	params Parameters
}

func (f *Flat) RecordPoll(votes bag.Bag[ids.ID]) bool {
	pollMode, numVotes := votes.Mode()
	switch {
	// AlphaConfidence is guaranteed to be >= AlphaPreference, so we must check
	// if the poll had enough votes to increase the confidence first.
	case numVotes >= f.params.AlphaConfidence:
		f.RecordSuccessfulPoll(pollMode)
		return true
	case numVotes >= f.params.AlphaPreference:
		f.RecordPollPreference(pollMode)
		return true
	default:
		f.RecordUnsuccessfulPoll()
		return false
	}
}
