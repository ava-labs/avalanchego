// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"fmt"
	"strings"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/prometheus/client_golang/prometheus"
)

type polls struct {
	log      logging.Logger
	numPolls prometheus.Gauge
	alpha    int
	m        map[uint32]poll
}

// Add to the current set of polls
// Returns true if the poll was registered correctly and the network sample
//         should be made.
func (p *polls) Add(requestID uint32, numPolled int) bool {
	poll, exists := p.m[requestID]
	if !exists {
		poll.alpha = p.alpha
		poll.numPolled = numPolled
		p.m[requestID] = poll

		p.numPolls.Set(float64(len(p.m))) // Tracks performance statistics
	}
	return !exists
}

// Vote registers the connections response to a query for [id]. If there was no
// query, or the response has already be registered, nothing is performed.
func (p *polls) Vote(requestID uint32, vdr ids.ShortID, vote ids.ID) (ids.Bag, bool) {
	p.log.Verbo("[polls.Vote] Vote: requestID: %d. validatorID: %s. Vote: %s", requestID, vdr, vote)
	poll, exists := p.m[requestID]
	if !exists {
		return ids.Bag{}, false
	}
	poll.Vote(vote)
	if poll.Finished() {
		delete(p.m, requestID)
		p.numPolls.Set(float64(len(p.m))) // Tracks performance statistics
		return poll.votes, true
	}
	p.m[requestID] = poll
	return ids.Bag{}, false
}

// CancelVote registers the connections failure to respond to a query for [id].
func (p *polls) CancelVote(requestID uint32, vdr ids.ShortID) (ids.Bag, bool) {
	p.log.Verbo("CancelVote received. requestID: %d. validatorID: %s. Vote: %s", requestID, vdr)
	poll, exists := p.m[requestID]
	if !exists {
		return ids.Bag{}, false
	}

	poll.CancelVote()
	if poll.Finished() {
		delete(p.m, requestID)
		p.numPolls.Set(float64(len(p.m))) // Tracks performance statistics
		return poll.votes, true
	}
	p.m[requestID] = poll
	return ids.Bag{}, false
}

func (p *polls) String() string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Current polls: (Size = %d)", len(p.m)))
	for requestID, poll := range p.m {
		sb.WriteString(fmt.Sprintf("\n    %d: %s", requestID, poll))
	}

	return sb.String()
}

// poll represents the current state of a network poll for a block
type poll struct {
	alpha     int
	votes     ids.Bag
	numPolled int
}

// Vote registers a vote for this poll
func (p *poll) CancelVote() {
	if p.numPolled > 0 {
		p.numPolled--
	}
}

// Vote registers a vote for this poll
func (p *poll) Vote(vote ids.ID) {
	if p.numPolled > 0 {
		p.numPolled--
		p.votes.Add(vote)
	}
}

// Finished returns true if the poll has completed, with no more required
// responses
func (p poll) Finished() bool {
	received := p.votes.Len()
	_, freq := p.votes.Mode()
	return p.numPolled == 0 || // All k nodes responded
		freq >= p.alpha || // An alpha majority has returned
		received+p.numPolled < p.alpha // An alpha majority can never return
}

func (p poll) String() string {
	return fmt.Sprintf("Waiting on %d chits", p.numPolled)
}
