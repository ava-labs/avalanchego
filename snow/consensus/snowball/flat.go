// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"github.com/chain4travel/caminogo/ids"
)

var (
	_ Factory   = &FlatFactory{}
	_ Consensus = &Flat{}
)

// FlatFactory implements Factory by returning a flat struct
type FlatFactory struct{}

func (FlatFactory) New() Consensus { return &Flat{} }

// Flat is a naive implementation of a multi-choice snowball instance
type Flat struct {
	// wraps the n-nary snowball logic
	nnarySnowball

	// params contains all the configurations of a snowball instance
	params Parameters
}

func (f *Flat) Initialize(params Parameters, choice ids.ID) {
	f.nnarySnowball.Initialize(params.BetaVirtuous, params.BetaRogue, choice)
	f.params = params
}

func (f *Flat) Parameters() Parameters { return f.params }

func (f *Flat) RecordPoll(votes ids.Bag) {
	if pollMode, numVotes := votes.Mode(); numVotes >= f.params.Alpha {
		f.RecordSuccessfulPoll(pollMode)
	} else {
		f.RecordUnsuccessfulPoll()
	}
}
