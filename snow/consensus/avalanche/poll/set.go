// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package poll

import (
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/metric"
)

var (
	_ Set  = (*set)(nil)
	_ Poll = (*poll)(nil)
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
	polls linkedhashmap.LinkedHashmap[uint32, pollHolder]
}

// NewSet returns a new empty set of polls
func NewSet(
	factory Factory,
	log logging.Logger,
	namespace string,
	reg prometheus.Registerer,
) Set {
	numPolls := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "polls",
		Help:      "Number of pending network polls",
	})
	if err := reg.Register(numPolls); err != nil {
		log.Error("failed to register polls statistics",
			zap.Error(err),
		)
	}

	durPolls, err := metric.NewAverager(
		namespace,
		"poll_duration",
		"time (in ns) this poll took to complete",
		reg,
	)
	if err != nil {
		log.Error("failed to register poll_duration statistics",
			zap.Error(err),
		)
	}

	return &set{
		log:      log,
		numPolls: numPolls,
		durPolls: durPolls,
		factory:  factory,
		polls:    linkedhashmap.New[uint32, pollHolder](),
	}
}

// Add to the current set of polls
// Returns true if the poll was registered correctly and the network sample
// should be made.
func (s *set) Add(requestID uint32, vdrs ids.NodeIDBag) bool {
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
func (s *set) Vote(requestID uint32, vdr ids.NodeID, votes []ids.ID) []ids.UniqueBag {
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

	s.log.Verbo("processing votes",
		zap.Stringer("validator", vdr),
		zap.Uint32("requestID", requestID),
		zap.Stringers("votes", votes),
	)

	p.Vote(vdr, votes)
	if !p.Finished() {
		return nil
	}

	var results []ids.UniqueBag

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
			zap.Uint32("requestID", requestID),
			zap.Stringer("poll", p),
		)
		s.durPolls.Observe(float64(time.Since(holder.StartTime())))
		s.numPolls.Dec() // decrease the metrics

		results = append(results, p.Result())
		s.polls.Delete(iter.Key()) // remove the poll from the current set
	}

	// only gets here if the poll has finished
	// results will have values if this and other newer polls have finished
	return results
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
