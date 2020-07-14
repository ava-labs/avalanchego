// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"

	"github.com/ava-labs/gecko/ids"
)

type noEarlyTermFactory struct{}

// NewNoEarlyTermFactory returns a factory that returns polls with no early
// termination
func NewNoEarlyTermFactory() Factory { return noEarlyTermFactory{} }

func (noEarlyTermFactory) New(vdrs ids.ShortSet) Poll {
	return &noEarlyTermPoll{polled: vdrs}
}

// noEarlyTermPoll finishes when all polled validators either respond to the
// query or a timeout occurs
type noEarlyTermPoll struct {
	votes  ids.Bag
	polled ids.ShortSet
}

// Vote registers a response for this poll
func (p *noEarlyTermPoll) Vote(vdr ids.ShortID, vote ids.ID) {
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
func (p *noEarlyTermPoll) Drop(vdr ids.ShortID) { p.polled.Remove(vdr) }

// Finished returns true when all validators have voted
func (p *noEarlyTermPoll) Finished() bool { return p.polled.Len() == 0 }

// Result returns the result of this poll
func (p *noEarlyTermPoll) Result() ids.Bag { return p.votes }

func (p *noEarlyTermPoll) String() string {
	return fmt.Sprintf("waiting on %s", p.polled)
}
