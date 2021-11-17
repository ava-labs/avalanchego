// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"container/list"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var _ Meter = &TimedMeter{}

// TimedMeter is a meter that discards old events
type TimedMeter struct {
	lock sync.Mutex
	// Can be used to fake time in tests
	Clock *mockable.Clock
	// Amount of time to keep a tick
	Duration time.Duration
	// TODO: Currently this list has an entry for each tick... This isn't really
	// sustainable at high tick numbers. We should be batching ticks with
	// similar times into the same bucket.
	tickList *list.List
}

// Tick implements the Meter interface
func (tm *TimedMeter) Tick() {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.tick()
}

// Ticks implements the Meter interface
func (tm *TimedMeter) Ticks() int {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	return tm.ticks()
}

func (tm *TimedMeter) init() {
	if tm.tickList == nil {
		tm.tickList = list.New()
	}
	if tm.Clock == nil {
		tm.Clock = &mockable.Clock{}
	}
}

func (tm *TimedMeter) tick() {
	tm.init()
	tm.tickList.PushBack(tm.Clock.Time())
}

func (tm *TimedMeter) ticks() int {
	tm.init()

	timeBound := tm.Clock.Time().Add(-tm.Duration)
	// removeExpiredHead returns false once there is nothing left to remove
	for tm.removeExpiredHead(timeBound) {
	}
	return tm.tickList.Len()
}

// Returns true if the head was removed, false otherwise
func (tm *TimedMeter) removeExpiredHead(t time.Time) bool {
	if tm.tickList.Len() == 0 {
		return false
	}

	head := tm.tickList.Front()
	headTime := head.Value.(time.Time)

	if headTime.Before(t) {
		tm.tickList.Remove(head)
		return true
	}
	return false
}
