// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
)

type earlyTermNoTraversalFactory struct {
	alpha int
}

// NewEarlyTermNoTraversalFactory returns a factory that returns polls with
// early termination, without doing DAG traversals
func NewEarlyTermNoTraversalFactory(alpha int) Factory {
	return &earlyTermNoTraversalFactory{alpha: alpha}
}

func (f *earlyTermNoTraversalFactory) New(vdrs ids.ShortSet) Poll {
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
	polled ids.ShortSet
	alpha  int
}

// Vote registers a response for this poll
func (p *earlyTermNoTraversalPoll) Vote(vdr ids.ShortID, vote ids.ID) {
	if !p.polled.Contains(vdr) {
		// if the validator wasn't polled or already responded to this poll, we
		// should just drop the vote
		return
	}

	// make sure that a validator can't respond multiple times
	p.polled.Remove(vdr)

	// track the votes the validator responded with
	p.votes.Add(vote)
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

func (p *earlyTermNoTraversalPoll) String() string {
	return fmt.Sprintf("waiting on %s", p.polled)
}
