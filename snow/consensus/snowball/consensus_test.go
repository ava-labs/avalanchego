// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

var (
	Red   = ids.Empty.Prefix(0)
	Blue  = ids.Empty.Prefix(1)
	Green = ids.Empty.Prefix(2)

	_ Consensus = (*Byzantine)(nil)
)

func NewByzantine(_ Factory, _ Parameters, choice ids.ID) Consensus {
	return &Byzantine{
		preference: choice,
	}
}

// Byzantine is a naive implementation of a multi-choice snowball instance
type Byzantine struct {
	// Hardcode the preference
	preference ids.ID
}

func (*Byzantine) Add(ids.ID) {}

func (b *Byzantine) Preference() ids.ID {
	return b.preference
}

func (*Byzantine) RecordPoll(bag.Bag[ids.ID]) bool {
	return false
}

func (*Byzantine) RecordUnsuccessfulPoll() {}

func (*Byzantine) Finalized() bool {
	return true
}

func (b *Byzantine) String() string {
	return b.preference.String()
}
