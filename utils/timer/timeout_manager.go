// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package timer

import (
	"container/list"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

type timeout struct {
	id      ids.ID
	handler func()
	timer   time.Time
}

// TimeoutManager is a manager for timeouts.
type TimeoutManager struct {
	lock        sync.Mutex
	duration    time.Duration // Amount of time before a timeout
	timeoutMap  map[ids.ID]*list.Element
	timeoutList *list.List
	timer       *Timer // Timer that will fire to clear the timeouts
}

// Initialize is a constructor b/c Golang, in its wisdom, doesn't ... have them?
func (tm *TimeoutManager) Initialize(duration time.Duration) {
	tm.duration = duration
	tm.timeoutMap = make(map[ids.ID]*list.Element)
	tm.timeoutList = list.New()
	tm.timer = NewTimer(tm.Timeout)
}

func (tm *TimeoutManager) Dispatch() { tm.timer.Dispatch() }

// Stop executing timeouts
func (tm *TimeoutManager) Stop() { tm.timer.Stop() }

// Put puts hash into the hash map
func (tm *TimeoutManager) Put(id ids.ID, handler func()) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.put(id, handler)
}

// Remove the item that no longer needs to be there.
func (tm *TimeoutManager) Remove(id ids.ID) {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.remove(id)
}

// Timeout registers a timeout
func (tm *TimeoutManager) Timeout() {
	tm.lock.Lock()
	defer tm.lock.Unlock()

	tm.timeout()
}

func (tm *TimeoutManager) timeout() {
	timeBound := time.Now().Add(-tm.duration)
	// removeExpiredHead returns false once there is nothing left to remove
	for {
		timeout := tm.removeExpiredHead(timeBound)
		if timeout == nil {
			break
		}

		// Don't execute a callback with a lock held
		tm.lock.Unlock()
		timeout()
		tm.lock.Lock()
	}
	tm.registerTimeout()
}

func (tm *TimeoutManager) put(id ids.ID, handler func()) {
	tm.remove(id)

	tm.timeoutMap[id] = tm.timeoutList.PushBack(timeout{
		id:      id,
		handler: handler,
		timer:   time.Now(),
	})

	if tm.timeoutList.Len() == 1 {
		tm.registerTimeout()
	}
}

func (tm *TimeoutManager) remove(id ids.ID) {
	e, exists := tm.timeoutMap[id]
	if !exists {
		return
	}
	delete(tm.timeoutMap, id)
	tm.timeoutList.Remove(e)
}

// Returns true if the head was removed, false otherwise
func (tm *TimeoutManager) removeExpiredHead(t time.Time) func() {
	if tm.timeoutList.Len() == 0 {
		return nil
	}

	e := tm.timeoutList.Front()
	head := e.Value.(timeout)

	headTime := head.timer
	if headTime.Before(t) {
		tm.remove(head.id)
		return head.handler
	}
	return nil
}

func (tm *TimeoutManager) registerTimeout() {
	if tm.timeoutList.Len() == 0 {
		// There are no pending timeouts
		tm.timer.Cancel()
		return
	}

	e := tm.timeoutList.Front()
	head := e.Value.(timeout)

	timeBound := time.Now().Add(-tm.duration)
	headTime := head.timer
	duration := headTime.Sub(timeBound)

	tm.timer.SetTimeoutIn(duration)
}
