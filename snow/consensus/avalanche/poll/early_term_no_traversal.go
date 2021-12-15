// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ Factory = &earlyTermNoTraversalFactory{}
	_ Poll    = &earlyTermNoTraversalPoll{}
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
	votes  ids.UniqueBag
	polled ids.ShortBag
	alpha  int
}

// Vote registers a response for this poll
func (p *earlyTermNoTraversalPoll) Vote(vdr ids.ShortID, votes []ids.ID) {
	count := p.polled.Count(vdr)
	// make sure that a validator can't respond multiple times
	p.polled.Remove(vdr)

	// track the votes the validator responded with
	for i := 0; i < count; i++ {
		p.votes.Add(uint(p.polled.Len()+i), votes...)
	}
}

// Finished returns true when all validators have voted
func (p *earlyTermNoTraversalPoll) Finished() bool {
	// If there are no outstanding queries, the poll is finished
	numPending := p.polled.Len()
	if numPending == 0 {
		return true
	}
	// If there are still enough pending responses to include another vertex,
	// then the poll must wait for more responses
	if numPending > p.alpha {
		return false
	}

	// Ignore any vertex that has already received alpha votes. To safely skip
	// DAG traversal, assume that all votes for vertices with less than alpha
	// votes will be applied to a single shared ancestor. In this case, the poll
	// can terminate early, iff there are not enough pending votes for this
	// ancestor to receive alpha votes.
	partialVotes := ids.BitSet(0)
	for _, vote := range p.votes.List() {
		if voters := p.votes.GetSet(vote); voters.Len() < p.alpha {
			partialVotes.Union(voters)
		}
	}
	return partialVotes.Len()+numPending < p.alpha
}

// Result returns the result of this poll
func (p *earlyTermNoTraversalPoll) Result() ids.UniqueBag { return p.votes }

func (p *earlyTermNoTraversalPoll) PrefixedString(prefix string) string {
	return fmt.Sprintf(
		"waiting on %s\n%sreceived %s",
		p.polled.PrefixedString(prefix),
		prefix,
		p.votes.PrefixedString(prefix),
	)
}

func (p *earlyTermNoTraversalPoll) String() string { return p.PrefixedString("") }
