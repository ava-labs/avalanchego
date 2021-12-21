// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

type earlyTermNoTraversalFactory struct {
	alpha int
}

// NewEarlyTermNoTraversalFactory returns a factory that returns polls with
// early termination, without doing DAG traversals
func NewEarlyTermNoTraversalFactory(alpha int) Factory {
	return &earlyTermNoTraversalFactory{alpha: alpha}
}

func (f *earlyTermNoTraversalFactory) New(vdrs ids.ShortBag) Poll {
	return &earlyTermNoTraversalPoll{
		polled: vdrs,
		alpha:  f.alpha,
	}
}

// earlyTermNoTraversalPoll finishes when any remaining validators can't change
// the result of the poll. However, does not terminate tightly with this bound.
// It terminates as quickly as it can without performing any DAG traversals.
type earlyTermNoTraversalPoll struct {
	votes  ids.Bag
	polled ids.ShortBag
	alpha  int
}

// Vote registers a response for this poll
func (p *earlyTermNoTraversalPoll) Vote(vdr ids.ShortID, vote ids.ID) {
	count := p.polled.Count(vdr)
	// make sure that a validator can't respond multiple times
	p.polled.Remove(vdr)

	// track the votes the validator responded with
	p.votes.AddCount(vote, count)
}

// Drop any future response for this poll
func (p *earlyTermNoTraversalPoll) Drop(vdr ids.ShortID) {
	p.polled.Remove(vdr)
}

// Finished returns true when all validators have voted
func (p *earlyTermNoTraversalPoll) Finished() bool {
	remaining := p.polled.Len()
	received := p.votes.Len()
	_, freq := p.votes.Mode()
	return remaining == 0 || // All k nodes responded
		freq >= p.alpha || // An alpha majority has returned
		received+remaining < p.alpha // An alpha majority can never return
}

// Result returns the result of this poll
func (p *earlyTermNoTraversalPoll) Result() ids.Bag { return p.votes }

func (p *earlyTermNoTraversalPoll) PrefixedString(prefix string) string {
	return fmt.Sprintf(
		"waiting on %s\n%sreceived %s",
		p.polled.PrefixedString(prefix),
		prefix,
		p.votes.PrefixedString(prefix),
	)
}

func (p *earlyTermNoTraversalPoll) String() string { return p.PrefixedString("") }
