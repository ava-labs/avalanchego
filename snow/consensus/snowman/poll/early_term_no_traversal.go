// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
)

var (
	errPollDurationVectorMetrics = errors.New("failed to register poll_duration vector metrics")
	errPollCountVectorMetrics    = errors.New("failed to register poll_count vector metrics")

	terminationReason    = "reason"
	exhaustedReason      = "exhausted"
	earlyFailReason      = "early_fail"
	earlyAlphaPrefReason = "early_alpha_pref"
	earlyAlphaConfReason = "early_alpha_conf"

	exhaustedLabel = prometheus.Labels{
		terminationReason: exhaustedReason,
	}
	earlyFailLabel = prometheus.Labels{
		terminationReason: earlyFailReason,
	}
	earlyAlphaPrefLabel = prometheus.Labels{
		terminationReason: earlyAlphaPrefReason,
	}
	earlyAlphaConfLabel = prometheus.Labels{
		terminationReason: earlyAlphaConfReason,
	}
)

type earlyTermNoTraversalMetrics struct {
	durExhaustedPolls      prometheus.Gauge
	durEarlyFailPolls      prometheus.Gauge
	durEarlyAlphaPrefPolls prometheus.Gauge
	durEarlyAlphaConfPolls prometheus.Gauge

	countExhaustedPolls      prometheus.Counter
	countEarlyFailPolls      prometheus.Counter
	countEarlyAlphaPrefPolls prometheus.Counter
	countEarlyAlphaConfPolls prometheus.Counter
}

func newEarlyTermNoTraversalMetrics(reg prometheus.Registerer) (*earlyTermNoTraversalMetrics, error) {
	pollCountVec := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "poll_count",
		Help: "Total # of terminated polls by reason",
	}, []string{terminationReason})
	if err := reg.Register(pollCountVec); err != nil {
		return nil, fmt.Errorf("%w: %w", errPollCountVectorMetrics, err)
	}
	durPollsVec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "poll_duration",
		Help: "time (in ns) polls took to complete by reason",
	}, []string{terminationReason})
	if err := reg.Register(durPollsVec); err != nil {
		return nil, fmt.Errorf("%w: %w", errPollDurationVectorMetrics, err)
	}

	return &earlyTermNoTraversalMetrics{
		durExhaustedPolls:        durPollsVec.With(exhaustedLabel),
		durEarlyFailPolls:        durPollsVec.With(earlyFailLabel),
		durEarlyAlphaPrefPolls:   durPollsVec.With(earlyAlphaPrefLabel),
		durEarlyAlphaConfPolls:   durPollsVec.With(earlyAlphaConfLabel),
		countExhaustedPolls:      pollCountVec.With(exhaustedLabel),
		countEarlyFailPolls:      pollCountVec.With(earlyFailLabel),
		countEarlyAlphaPrefPolls: pollCountVec.With(earlyAlphaPrefLabel),
		countEarlyAlphaConfPolls: pollCountVec.With(earlyAlphaConfLabel),
	}, nil
}

func (m *earlyTermNoTraversalMetrics) observeExhausted(duration time.Duration) {
	m.durExhaustedPolls.Add(float64(duration.Nanoseconds()))
	m.countExhaustedPolls.Inc()
}

func (m *earlyTermNoTraversalMetrics) observeEarlyFail(duration time.Duration) {
	m.durEarlyFailPolls.Add(float64(duration.Nanoseconds()))
	m.countEarlyFailPolls.Inc()
}

func (m *earlyTermNoTraversalMetrics) observeEarlyAlphaPref(duration time.Duration) {
	m.durEarlyAlphaPrefPolls.Add(float64(duration.Nanoseconds()))
	m.countEarlyAlphaPrefPolls.Inc()
}

func (m *earlyTermNoTraversalMetrics) observeEarlyAlphaConf(duration time.Duration) {
	m.durEarlyAlphaConfPolls.Add(float64(duration.Nanoseconds()))
	m.countEarlyAlphaConfPolls.Inc()
}

type earlyTermNoTraversalFactory struct {
	alphaPreference int
	alphaConfidence int

	metrics *earlyTermNoTraversalMetrics
}

// NewEarlyTermNoTraversalFactory returns a factory that returns polls with
// early termination, without doing DAG traversals
func NewEarlyTermNoTraversalFactory(
	alphaPreference int,
	alphaConfidence int,
	reg prometheus.Registerer,
) (Factory, error) {
	metrics, err := newEarlyTermNoTraversalMetrics(reg)
	if err != nil {
		return nil, err
	}

	return &earlyTermNoTraversalFactory{
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
		metrics:         metrics,
	}, nil
}

func (f *earlyTermNoTraversalFactory) New(vdrs bag.Bag[ids.NodeID]) Poll {
	return &earlyTermNoTraversalPoll{
		polled:          vdrs,
		alphaPreference: f.alphaPreference,
		alphaConfidence: f.alphaConfidence,
		metrics:         f.metrics,
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

	metrics  *earlyTermNoTraversalMetrics
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
		p.metrics.observeExhausted(time.Since(p.start))
		return true // Case 1
	}

	received := p.votes.Len()
	maxPossibleVotes := received + remaining
	if maxPossibleVotes < p.alphaPreference {
		p.finished = true
		p.metrics.observeEarlyFail(time.Since(p.start))
		return true // Case 2
	}

	_, freq := p.votes.Mode()
	if freq >= p.alphaPreference && maxPossibleVotes < p.alphaConfidence {
		p.finished = true
		p.metrics.observeEarlyAlphaPref(time.Since(p.start))
		return true // Case 3
	}

	if freq >= p.alphaConfidence {
		p.finished = true
		p.metrics.observeEarlyAlphaConf(time.Since(p.start))
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
