// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

var _ Consensus = (*Flat)(nil)

func NewFlat(params Parameters, choice ids.ID) Consensus {
	return &Flat{
		nnarySnowball: newNnarySnowball(params.BetaVirtuous, params.BetaRogue, choice),
		params:        params,
	}
}

// Flat is a naive implementation of a multi-choice snowball instance
type Flat struct {
	// wraps the n-nary snowball logic
	nnarySnowball

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
