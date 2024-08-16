// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
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

type earlyTermTraversalMetrics struct {
	durExhaustedPolls      prometheus.Gauge
	durEarlyFailPolls      prometheus.Gauge
	durEarlyAlphaPrefPolls prometheus.Gauge
	durEarlyAlphaConfPolls prometheus.Gauge

	countExhaustedPolls      prometheus.Counter
	countEarlyFailPolls      prometheus.Counter
	countEarlyAlphaPrefPolls prometheus.Counter
	countEarlyAlphaConfPolls prometheus.Counter
}

func newEarlyTermTraversalMetrics(reg prometheus.Registerer) (*earlyTermTraversalMetrics, error) {
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

	return &earlyTermTraversalMetrics{
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

func (m *earlyTermTraversalMetrics) observeExhausted(duration time.Duration) {
	m.durExhaustedPolls.Add(float64(duration.Nanoseconds()))
	m.countExhaustedPolls.Inc()
}

func (m *earlyTermTraversalMetrics) observeEarlyFail(duration time.Duration) {
	m.durEarlyFailPolls.Add(float64(duration.Nanoseconds()))
	m.countEarlyFailPolls.Inc()
}

func (m *earlyTermTraversalMetrics) observeEarlyAlphaPref(duration time.Duration) {
	m.durEarlyAlphaPrefPolls.Add(float64(duration.Nanoseconds()))
	m.countEarlyAlphaPrefPolls.Inc()
}

func (m *earlyTermTraversalMetrics) observeEarlyAlphaConf(duration time.Duration) {
	m.durEarlyAlphaConfPolls.Add(float64(duration.Nanoseconds()))
	m.countEarlyAlphaConfPolls.Inc()
}

type earlyTermTraversalFactory struct {
	alphaPreference int
	alphaConfidence int
	bt              snow.BlockTraversal
	metrics         *earlyTermTraversalMetrics
}

// NewEarlyTermTraversalFactory returns a factory that returns polls with
// early termination, without doing DAG traversals
func NewEarlyTermTraversalFactory(
	alphaPreference int,
	alphaConfidence int,
	reg prometheus.Registerer,
	bt snow.BlockTraversal,
) (Factory, error) {
	metrics, err := newEarlyTermTraversalMetrics(reg)
	if err != nil {
		return nil, err
	}

	return &earlyTermTraversalFactory{
		bt:              bt,
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
		metrics:         metrics,
	}, nil
}

func (f *earlyTermTraversalFactory) New(vdrs bag.Bag[ids.NodeID]) Poll {
	return &earlyTermTraversalPoll{
		bt:              f.bt,
		polled:          vdrs,
		alphaPreference: f.alphaPreference,
		alphaConfidence: f.alphaConfidence,
		metrics:         f.metrics,
		start:           time.Now(),
	}
}

// earlyTermTraversalPoll finishes when any remaining validators can't change
// the result of the poll for all the votes and transitive votes.
type earlyTermTraversalPoll struct {
	votes           bag.Bag[ids.ID]
	polled          bag.Bag[ids.NodeID]
	alphaPreference int
	alphaConfidence int
	bt              snow.BlockTraversal
	metrics         *earlyTermTraversalMetrics
	start           time.Time
	finished        bool
}

// Vote registers a response for this poll
func (p *earlyTermTraversalPoll) Vote(vdr ids.NodeID, vote ids.ID) {
	count := p.polled.Count(vdr)
	// make sure that a validator can't respond multiple times
	p.polled.Remove(vdr)

	// track the votes the validator responded with
	p.votes.AddCount(vote, count)
}

// Drop any future response for this poll
func (p *earlyTermTraversalPoll) Drop(vdr ids.NodeID) {
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
func (p *earlyTermTraversalPoll) Finished() bool {
	if p.finished {
		return true
	}

	remaining := p.polled.Len()
	received := p.votes.Len()
	maxPossibleVotes := received + remaining

	if remaining == 0 {
		p.finished = true
		p.metrics.observeExhausted(time.Since(p.start))
		return true // Case 1
	}

	if maxPossibleVotes < p.alphaPreference {
		p.finished = true
		p.metrics.observeEarlyFail(time.Since(p.start))
		return true // Case 2
	}

	//    v
	//   /
	//  u
	// We build a vote graph where each vertex represents a block ID.
	// A vertex 'v' is a parent of vertex 'u' if the ID of 'u' corresponds
	// to a block that is the successive block of the corresponding block for 'v'.
	votesGraph := buildVotesGraph(p.bt.GetParent, p.votes)

	// If vertex 'v' is a parent of vertex 'u', then a vote for the ID of vertex 'u'
	// should also be considered as a vote for the ID of the vertex 'v'.
	transitiveVotes := computeTransitiveVoteCountGraph(&votesGraph, p.votes)

	//     v
	//   /   \
	//  u     w
	// If two competing blocks 'u', 'w' are potential successors to a block 'v',
	// snowman would instantiate a unary snowflake instance on the prefix of 'u' and 'w'.
	// The prefix inherits the votes for the IDs of 'u' and 'w'.
	// We therefore compute the transitive votes for all prefixes of IDs
	// for each bifurcation in the transitive vote graph.
	transitiveVotesForPrefixes := transitiveVotesForPrefixes(&votesGraph, transitiveVotes)

	// We wish to compute the votes for snowflake instances, no matter if they correspond to an actual block ID,
	// or a unary snowflake instance for a shared prefix between a bifurcation of two competing blocks.
	// For that, only the number of votes and existence of such snowflake instances matters, and nothing else.
	voteCountsForIDsOrPrefixes := aggregateVotesFromPrefixesAndIDs(transitiveVotesForPrefixes, transitiveVotes)

	// Given the aforementioned votes, we wish to see whether there exists a snowflake instance
	// that can benefit from waiting for more invocations of Vote().
	// We therefore check each amount of votes separately and see if voting for that snowflake instance
	// should terminate, as it cannot be improved by further voting.
	weCantImproveVoteForSomeIDOrPrefix := make(booleans, len(voteCountsForIDsOrPrefixes))
	for i, votesForID := range voteCountsForIDsOrPrefixes {
		shouldTerminate := p.shouldTerminateDueToConfidence(votesForID, maxPossibleVotes, remaining)
		weCantImproveVoteForSomeIDOrPrefix[i] = shouldTerminate
	}

	// We should terminate the poll only when voting for all snowflake instances should terminate.
	if weCantImproveVoteForSomeIDOrPrefix.allTrue() && len(weCantImproveVoteForSomeIDOrPrefix) > 0 {
		p.finished = true
	}

	return p.finished
}

func (p *earlyTermTraversalPoll) shouldTerminateDueToConfidence(freq int, maxPossibleVotes, remaining int) bool {
	if freq+remaining < p.alphaPreference {
		return true // Case 2
	}

	if freq >= p.alphaPreference && maxPossibleVotes < p.alphaConfidence {
		p.metrics.observeEarlyAlphaPref(time.Since(p.start))
		return true // Case 3
	}

	if freq >= p.alphaConfidence {
		p.metrics.observeEarlyAlphaConf(time.Since(p.start))
		return true // Case 4
	}

	return false
}

// Result returns the result of this poll
func (p *earlyTermTraversalPoll) Result() bag.Bag[ids.ID] {
	return p.votes
}

func (p *earlyTermTraversalPoll) PrefixedString(prefix string) string {
	return fmt.Sprintf(
		"waiting on %s\n%sreceived %s",
		p.polled.PrefixedString(prefix),
		prefix,
		p.votes.PrefixedString(prefix),
	)
}

func (p *earlyTermTraversalPoll) String() string {
	return p.PrefixedString("")
}

func aggregateVotesFromPrefixesAndIDs(transitiveVotesForPrefixes map[string]int, transitiveVotes bag.Bag[ids.ID]) []int {
	voteCountsForIDsOrPrefixes := make([]int, 0, len(transitiveVotesForPrefixes)+len(transitiveVotes.List()))

	for _, id := range transitiveVotes.List() {
		votesForID := transitiveVotes.Count(id)
		voteCountsForIDsOrPrefixes = append(voteCountsForIDsOrPrefixes, votesForID)
	}

	for _, voteCount := range transitiveVotesForPrefixes {
		voteCountsForIDsOrPrefixes = append(voteCountsForIDsOrPrefixes, voteCount)
	}
	return voteCountsForIDsOrPrefixes
}

func transitiveVotesForPrefixes(votesGraph *voteGraph, transitiveVotes bag.Bag[ids.ID]) map[string]int {
	votesForPrefix := make(map[string]int)
	votesGraph.traverse(func(v *voteVertex) {
		descendanstIDs := descendantIDsOfVertex(v)
		pg := longestSharedPrefixes(descendanstIDs)
		// Each shared prefix is associated to a bunch of IDs.
		// Sum up all the transitive votes for these blocks,
		// and return all such shared prefixes indexed by the underlying transitive descendant IDs..
		pg.bifurcationsWithCommonPrefix(func(ids []ids.ID, _ []uint8) {
			key := concatIDs(ids)
			count := sumVotesFromIDs(ids, transitiveVotes)
			votesForPrefix[key] = count
		})
	})
	return votesForPrefix
}

func descendantIDsOfVertex(v *voteVertex) []ids.ID {
	descendanstIDs := make([]ids.ID, len(v.descendants))
	for i, child := range v.descendants {
		descendanstIDs[i] = child.id
	}
	return descendanstIDs
}

func concatIDs(ids []ids.ID) string {
	var bb bytes.Buffer
	for _, id := range ids {
		bb.WriteString(id.String())
		bb.WriteString(" ")
	}
	return bb.String()
}

func sumVotesFromIDs(ids []ids.ID, transitiveVotes bag.Bag[ids.ID]) int {
	var count int
	for _, id := range ids {
		count += transitiveVotes.Count(id)
	}
	return count
}

// booleans represents zero or more booleans
type booleans []bool

// allTrue returns whether all booleans are true
func (bs booleans) allTrue() bool {
	for _, b := range bs {
		if !b {
			return false
		}
	}
	return true
}
