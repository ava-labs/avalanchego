// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/linked"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
)

var (
	errFailedPollsMetric         = errors.New("failed to register polls metric")
	errFailedPollDurationMetrics = errors.New("failed to register poll_duration metrics")
)

type pollHolder interface {
	GetPoll() Poll
	StartTime() time.Time
}

type poll struct {
	Poll
	start time.Time
}

func (p poll) GetPoll() Poll {
	return p
}

func (p poll) StartTime() time.Time {
	return p.start
}

type set struct {
	log      logging.Logger
	numPolls prometheus.Gauge
	durPolls metric.Averager
	factory  Factory
	// maps requestID -> poll
	polls *linked.Hashmap[uint32, pollHolder]
}

// NewSet returns a new empty set of polls
func NewSet(
	factory Factory,
	log logging.Logger,
	reg prometheus.Registerer,
) (Set, error) {
	numPolls := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "polls",
		Help: "Number of pending network polls",
	})
	if err := reg.Register(numPolls); err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedPollsMetric, err)
	}

	durPolls, err := metric.NewAverager(
		"poll_duration",
		"time (in ns) this poll took to complete",
		reg,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedPollDurationMetrics, err)
	}

	return &set{
		log:      log,
		numPolls: numPolls,
		durPolls: durPolls,
		factory:  factory,
		polls:    linked.NewHashmap[uint32, pollHolder](),
	}, nil
}

// Add to the current set of polls
// Returns true if the poll was registered correctly and the network sample
// should be made.
func (s *set) Add(requestID uint32, vdrs bag.Bag[ids.NodeID]) bool {
	if _, exists := s.polls.Get(requestID); exists {
		s.log.Debug("dropping poll",
			zap.String("reason", "duplicated request"),
			zap.Uint32("requestID", requestID),
		)
		return false
	}

	s.log.Verbo("creating poll",
		zap.Uint32("requestID", requestID),
		zap.Stringer("validators", &vdrs),
	)

	s.polls.Put(requestID, poll{
		Poll:  s.factory.New(vdrs), // create the new poll
		start: time.Now(),
	})
	s.numPolls.Inc() // increase the metrics
	return true
}

// Vote registers the connections response to a query for [id]. If there was no
// query, or the response has already be registered, nothing is performed.
func (s *set) Vote(requestID uint32, vdr ids.NodeID, vote ids.ID) []bag.Bag[ids.ID] {
	holder, exists := s.polls.Get(requestID)
	if !exists {
		s.log.Verbo("dropping vote",
			zap.String("reason", "unknown poll"),
			zap.Stringer("validator", vdr),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	p := holder.GetPoll()

	s.log.Verbo("processing vote",
		zap.Stringer("validator", vdr),
		zap.Uint32("requestID", requestID),
		zap.Stringer("vote", vote),
	)

	p.Vote(vdr, vote)
	if !p.Finished() {
		return nil
	}

	return s.processFinishedPolls()
}

// processFinishedPolls checks for other dependent finished polls and returns them all if finished
func (s *set) processFinishedPolls() []bag.Bag[ids.ID] {
	var results []bag.Bag[ids.ID]

	// iterate from oldest to newest
	iter := s.polls.NewIterator()
	for iter.Next() {
		holder := iter.Value()
		p := holder.GetPoll()
		if !p.Finished() {
			// since we're iterating from oldest to newest, if the next poll has not finished,
			// we can break and return what we have so far
			break
		}

		s.log.Verbo("poll finished",
			zap.Uint32("requestID", iter.Key()),
			zap.Stringer("poll", holder.GetPoll()),
		)
		s.durPolls.Observe(float64(time.Since(holder.StartTime())))
		s.numPolls.Dec() // decrease the metrics

		results = append(results, p.Result())
		s.polls.Delete(iter.Key())
	}

	// only gets here if the poll has finished
	// results will have values if this and other newer polls have finished
	return results
}

// Drop registers the connections response to a query for [id]. If there was no
// query, or the response has already be registered, nothing is performed.
func (s *set) Drop(requestID uint32, vdr ids.NodeID) []bag.Bag[ids.ID] {
	holder, exists := s.polls.Get(requestID)
	if !exists {
		s.log.Verbo("dropping vote",
			zap.String("reason", "unknown poll"),
			zap.Stringer("validator", vdr),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	s.log.Verbo("processing dropped vote",
		zap.Stringer("validator", vdr),
		zap.Uint32("requestID", requestID),
	)

	poll := holder.GetPoll()

	poll.Drop(vdr)
	if !poll.Finished() {
		return nil
	}

	return s.processFinishedPolls()
}

// Len returns the number of outstanding polls
func (s *set) Len() int {
	return s.polls.Len()
}

func (s *set) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("current polls: (Size = %d)", s.polls.Len()))
	iter := s.polls.NewIterator()
	for iter.Next() {
		requestID := iter.Key()
		poll := iter.Value().(Poll)
		sb.WriteString(fmt.Sprintf("\n    RequestID %d:\n        %s", requestID, poll.PrefixedString("        ")))
	}
	return sb.String()
}
