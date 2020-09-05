// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"

	"github.com/ava-labs/gecko/ids"
)

// CountingTracker is an interface for tracking peers' usage of a discrete resource
type CountingTracker interface {
	Add(ids.ShortID)                               // increments total count taken by ID
	AddPool(ids.ShortID)                           // increments pool messages taken by ID
	Remove(ids.ShortID)                            // removes a message taken by ID
	OutstandingCount(ids.ShortID) (uint32, uint32) // returns the total count and pool count
	PoolCount() uint32                             // returns the total count of messages taken from the pool
}

// msgTracker implements CountingTracker to keep track of pending messages to peers
type msgTracker struct {
	lock sync.Mutex

	// Track peers outstanding messages
	msgSpenders map[[20]byte]*msgCount
	poolCount   uint32
}

// NewMessageTracker returns a CountingTracker to track
// pending messages from peers
func NewMessageTracker() CountingTracker {
	return &msgTracker{
		msgSpenders: make(map[[20]byte]*msgCount),
	}
}

// Add implements CountingTracker
func (et *msgTracker) Add(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	msgCount := et.getCount(validatorID)
	msgCount.totalMessages++
}

func (et *msgTracker) AddPool(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	msgCount := et.getCount(validatorID)
	msgCount.totalMessages++
	msgCount.poolMessages++
	et.poolCount++
}

// Remove implements CountingTracker
func (et *msgTracker) Remove(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	msgCount := et.getCount(validatorID)

	msgCount.totalMessages--
	if msgCount.poolMessages > 0 {
		msgCount.poolMessages--
		et.poolCount--
	}

	if msgCount.totalMessages == 0 {
		delete(et.msgSpenders, validatorID.Key())
	}
}

// OutstandingCount implements CountingTracker
func (et *msgTracker) OutstandingCount(validatorID ids.ShortID) (uint32, uint32) {
	et.lock.Lock()
	defer et.lock.Unlock()

	msgCount := et.getCount(validatorID)
	return msgCount.totalMessages, msgCount.poolMessages
}

// PoolCount implements CountingTracker
func (et *msgTracker) PoolCount() uint32 { return et.poolCount }

// getCount returns the message count for [validatorID]
// assumes the lock is held
func (et *msgTracker) getCount(validatorID ids.ShortID) *msgCount {
	key := validatorID.Key()
	if msgCount, exists := et.msgSpenders[key]; exists {
		return msgCount
	}

	msgCount := &msgCount{}
	et.msgSpenders[key] = msgCount
	return msgCount
}

type msgCount struct {
	totalMessages, poolMessages uint32
}
