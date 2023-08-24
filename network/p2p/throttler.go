// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
	// Handle returns if a message from [nodeID] should be handled or not.
	Handle(nodeID ids.NodeID) bool
}

// window is used internally by SlidingWindowThrottler to represent the amount
// of hits from a node in a given evaluation period
type window struct {
	start time.Time
	hits  map[ids.NodeID]int
}

// NewSlidingWindowThrottler returns a new instance of SlidingWindowThrottler
func NewSlidingWindowThrottler(period time.Duration, limit int) *SlidingWindowThrottler {
	now := time.Now()
	return &SlidingWindowThrottler{
		period: period,
		limit:  limit,
		current: window{
			start: now,
			hits:  make(map[ids.NodeID]int),
		},
		previous: window{
			start: now.Add(-period),
			hits:  make(map[ids.NodeID]int),
		},
	}
}

// SlidingWindowThrottler is an implementation of the sliding window throttling
// algorithm.
type SlidingWindowThrottler struct {
	period time.Duration
	limit  int

	lock     sync.Mutex
	current  window
	previous window
	clock    mockable.Clock
}

// Handle estimates the amount of requests made in the last two windows.
// This is computed as the amount of calls made in the current evaluation +
// the weighted amount of calls made in the previous evaluation period
func (s *SlidingWindowThrottler) Handle(nodeID ids.NodeID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	// check if the current evaluation period is over
	now := s.clock.Time()
	if now.After(s.current.start.Add(s.period)) {
		s.previous = s.current
		s.current = window{
			start: now,
			hits:  make(map[ids.NodeID]int),
		}
	}

	offset := s.clock.Time().Sub(s.current.start)
	weight := float64(s.period-offset) / float64(s.period)

	if weight*float64(s.previous.hits[nodeID])+float64(s.current.hits[nodeID]) < float64(s.limit) {
		s.current.hits[nodeID]++
		return true
	}

	return false
}
