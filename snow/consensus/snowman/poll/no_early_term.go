// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

type noEarlyTermFactory struct{}

// NewNoEarlyTermFactory returns a factory that returns polls with no early
// termination
func NewNoEarlyTermFactory() Factory { return noEarlyTermFactory{} }

func (noEarlyTermFactory) New(vdrs ids.ShortBag) Poll {
	return &noEarlyTermPoll{polled: vdrs}
}

// noEarlyTermPoll finishes when all polled validators either respond to the
// query or a timeout occurs
type noEarlyTermPoll struct {
	votes  ids.Bag
	polled ids.ShortBag
}

// Vote registers a response for this poll
func (p *noEarlyTermPoll) Vote(vdr ids.ShortID, vote ids.ID) {
	count := p.polled.Count(vdr)
	// make sure that a validator can't respond multiple times
	p.polled.Remove(vdr)

	// track the votes the validator responded with
	p.votes.AddCount(vote, count)
}

// Drop any future response for this poll
func (p *noEarlyTermPoll) Drop(vdr ids.ShortID) { p.polled.Remove(vdr) }

// Finished returns true when all validators have voted
func (p *noEarlyTermPoll) Finished() bool {
	return p.polled.Len() == 0
}

// Result returns the result of this poll
func (p *noEarlyTermPoll) Result() ids.Bag { return p.votes }

func (p *noEarlyTermPoll) PrefixedString(prefix string) string {
	return fmt.Sprintf(
		"waiting on %s\n%sreceived %s",
		p.polled.PrefixedString(prefix),
		prefix,
		p.votes.PrefixedString(prefix),
	)
}

func (p *noEarlyTermPoll) String() string { return p.PrefixedString("") }
