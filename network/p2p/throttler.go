// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ Throttler = (*SlidingWindowThrottler)(nil)

type Throttler interface {
	// Handle returns true if a message from [nodeID] should be handled.
	Handle(nodeID ids.NodeID) bool
}

// NewSlidingWindowThrottler returns a new instance of SlidingWindowThrottler.
// Nodes are throttled if they exceed [limit] messages during an interval of
// time over [period].
// [period] and [limit] should both be > 0.
func NewSlidingWindowThrottler(period time.Duration, limit int) *SlidingWindowThrottler {
	now := time.Now()
	return &SlidingWindowThrottler{
		period: period,
		limit:  float64(limit),
		windows: [2]window{
			{
				start: now,
				hits:  make(map[ids.NodeID]float64),
			},
			{
				start: now.Add(-period),
				hits:  make(map[ids.NodeID]float64),
			},
		},
	}
}

// window is used internally by SlidingWindowThrottler to represent the amount
// of hits from a node in the evaluation period beginning at [start]
type window struct {
	start time.Time
	hits  map[ids.NodeID]float64
}

// SlidingWindowThrottler is an implementation of the sliding window throttling
// algorithm.
type SlidingWindowThrottler struct {
	period time.Duration
	limit  float64
	clock  mockable.Clock

	lock    sync.Mutex
	current int
	windows [2]window
}

// Handle returns true if the amount of calls received in the last [s.period]
// time is less than [s.limit]
//
// This is calculated by adding the current period's count to a weighted count
// of the previous period.
func (s *SlidingWindowThrottler) Handle(nodeID ids.NodeID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	// The current window becomes the previous window if the current evaluation
	// period is over
	now := s.clock.Time()
	sinceUpdate := now.Sub(s.windows[s.current].start)
	if sinceUpdate >= 2*s.period {
		s.rotate(now.Add(-s.period))
	}
	if sinceUpdate >= s.period {
		s.rotate(now)
		sinceUpdate = 0
	}

	currentHits := s.windows[s.current].hits
	current := currentHits[nodeID]
	currentHits[nodeID]++

	previousFraction := float64(s.period-sinceUpdate) / float64(s.period)
	previous := s.windows[1-s.current].hits[nodeID]
	estimatedHits := current + previousFraction*previous
	if estimatedHits >= s.limit {
		// The peer has sent too many requests, drop this request.
		return false
	}

	return true
}

func (s *SlidingWindowThrottler) rotate(t time.Time) {
	s.current = 1 - s.current
	s.windows[s.current] = window{
		start: t,
		hits:  make(map[ids.NodeID]float64),
	}
}

func (s *SlidingWindowThrottler) setLimit(limit float64) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.limit = limit
}
