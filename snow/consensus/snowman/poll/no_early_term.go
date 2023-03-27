// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

type noEarlyTermFactory struct{}

// NewNoEarlyTermFactory returns a factory that returns polls with no early
// termination
func NewNoEarlyTermFactory() Factory {
	return noEarlyTermFactory{}
}

func (noEarlyTermFactory) New(vdrs bag.Bag[ids.NodeID]) Poll {
	return &noEarlyTermPoll{polled: vdrs}
}

// noEarlyTermPoll finishes when all polled validators either respond to the
// query or a timeout occurs
type noEarlyTermPoll struct {
	votes  bag.Bag[ids.ID]
	polled bag.Bag[ids.NodeID]
}

// Vote registers a response for this poll
func (p *noEarlyTermPoll) Vote(vdr ids.NodeID, vote ids.ID) {
	count := p.polled.Count(vdr)
	// make sure that a validator can't respond multiple times
	p.polled.Remove(vdr)

	// track the votes the validator responded with
	p.votes.AddCount(vote, count)
}

// Drop any future response for this poll
func (p *noEarlyTermPoll) Drop(vdr ids.NodeID) {
	p.polled.Remove(vdr)
}

// Finished returns true when all validators have voted
func (p *noEarlyTermPoll) Finished() bool {
	return p.polled.Len() == 0
}

// Result returns the result of this poll
func (p *noEarlyTermPoll) Result() bag.Bag[ids.ID] {
	return p.votes
}

func (p *noEarlyTermPoll) PrefixedString(prefix string) string {
	return fmt.Sprintf(
		"waiting on %s\n%sreceived %s",
		p.polled.PrefixedString(prefix),
		prefix,
		p.votes.PrefixedString(prefix),
	)
}

func (p *noEarlyTermPoll) String() string {
	return p.PrefixedString("")
}
