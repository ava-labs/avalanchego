// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/metric"
)

type earlyTermNoTraversalFactory struct {
	alphaPreference int
	alphaConfidence int
	durPolls        []metric.Averager
}

// NewEarlyTermNoTraversalFactory returns a factory that returns polls with
// early termination, without doing DAG traversals
func NewEarlyTermNoTraversalFactory(
	alphaPreference int,
	alphaConfidence int,
	reg prometheus.Registerer,
) (Factory, error) {
	f := &earlyTermNoTraversalFactory{
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
	}

	durPolls1, err := metric.NewAverager(
		"poll_duration_case_1",
		"time (in ns) this poll took to complete",
		reg,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedPollDurationMetrics, err)
	}
	durPolls2, err := metric.NewAverager(
		"poll_duration_case_2",
		"time (in ns) this poll took to complete",
		reg,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedPollDurationMetrics, err)
	}
	durPolls3, err := metric.NewAverager(
		"poll_duration_case_3",
		"time (in ns) this poll took to complete",
		reg,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedPollDurationMetrics, err)
	}
	durPolls4, err := metric.NewAverager(
		"poll_duration_case_4",
		"time (in ns) this poll took to complete",
		reg,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedPollDurationMetrics, err)
	}
	f.durPolls = []metric.Averager{durPolls1, durPolls2, durPolls3, durPolls4}

	return f, nil
}

func (f *earlyTermNoTraversalFactory) New(vdrs bag.Bag[ids.NodeID]) Poll {
	return &earlyTermNoTraversalPoll{
		polled:          vdrs,
		alphaPreference: f.alphaPreference,
		alphaConfidence: f.alphaConfidence,
		durPolls:        f.durPolls,
		start:           time.Now(),
	}
}

// earlyTermNoTraversalPoll finishes when any remaining validators can't change
// the result of the poll. However, does not terminate tightly with this bound.
// It terminates as quickly as it can without performing any DAG traversals.
type earlyTermNoTraversalPoll struct {
	votes           bag.Bag[ids.ID]
	polled          bag.Bag[ids.NodeID]
	alphaPreference int
	alphaConfidence int

	durPolls []metric.Averager
	start    time.Time
	finished bool
}

// Vote registers a response for this poll
func (p *earlyTermNoTraversalPoll) Vote(vdr ids.NodeID, vote ids.ID) {
	count := p.polled.Count(vdr)
	// make sure that a validator can't respond multiple times
	p.polled.Remove(vdr)

	// track the votes the validator responded with
	p.votes.AddCount(vote, count)
}

// Drop any future response for this poll
func (p *earlyTermNoTraversalPoll) Drop(vdr ids.NodeID) {
	p.polled.Remove(vdr)
}

// Finished returns true when one of the following conditions is met.
//
//  1. There are no outstanding votes.
//  2. It is impossible for the poll to achieve an alphaPreference majority
//     after applying transitive voting.
//  3. A single element has achieved an alphaPreference majority and it is
//     impossible for it to achieve an alphaConfidence majority after applying
//     transitive voting.
//  4. A single element has achieved an alphaConfidence majority.
func (p *earlyTermNoTraversalPoll) Finished() bool {
	if p.finished {
		return true
	}

	remaining := p.polled.Len()
	if remaining == 0 {
		p.finished = true
		p.durPolls[0].Observe(float64(time.Since(p.start).Nanoseconds()))
		return true // Case 1
	}

	received := p.votes.Len()
	maxPossibleVotes := received + remaining
	if maxPossibleVotes < p.alphaPreference {
		p.finished = true
		p.durPolls[1].Observe(float64(time.Since(p.start).Nanoseconds()))
		return true // Case 2
	}

	_, freq := p.votes.Mode()
	if freq >= p.alphaPreference && maxPossibleVotes < p.alphaConfidence {
		p.finished = true
		p.durPolls[2].Observe(float64(time.Since(p.start).Nanoseconds()))
		return true // Case 3
	}

	if freq >= p.alphaConfidence {
		p.finished = true
		p.durPolls[3].Observe(float64(time.Since(p.start).Nanoseconds()))
		return true // Case 4
	}

	return false
}

// Result returns the result of this poll
func (p *earlyTermNoTraversalPoll) Result() bag.Bag[ids.ID] {
	return p.votes
}

func (p *earlyTermNoTraversalPoll) PrefixedString(prefix string) string {
	return fmt.Sprintf(
		"waiting on %s\n%sreceived %s",
		p.polled.PrefixedString(prefix),
		prefix,
		p.votes.PrefixedString(prefix),
	)
}

func (p *earlyTermNoTraversalPoll) String() string {
	return p.PrefixedString("")
}
