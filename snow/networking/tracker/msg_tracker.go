// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"

	"github.com/ava-labs/avalanchego/ids"
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
func (mt *msgTracker) Add(validatorID ids.ShortID) {
	mt.lock.Lock()
	defer mt.lock.Unlock()

	msgCount := mt.getCount(validatorID)
	msgCount.totalMessages++
}

func (mt *msgTracker) AddPool(validatorID ids.ShortID) {
	mt.lock.Lock()
	defer mt.lock.Unlock()

	msgCount := mt.getCount(validatorID)
	msgCount.totalMessages++
	msgCount.poolMessages++
	mt.poolCount++
}

// Remove implements CountingTracker
func (mt *msgTracker) Remove(validatorID ids.ShortID) {
	mt.lock.Lock()
	defer mt.lock.Unlock()

	msgCount := mt.getCount(validatorID)

	msgCount.totalMessages--
	if msgCount.poolMessages > 0 {
		msgCount.poolMessages--
		mt.poolCount--
	}

	if msgCount.totalMessages == 0 {
		delete(mt.msgSpenders, validatorID.Key())
	}
}

// OutstandingCount implements CountingTracker
func (mt *msgTracker) OutstandingCount(validatorID ids.ShortID) (uint32, uint32) {
	mt.lock.Lock()
	defer mt.lock.Unlock()

	msgCount := mt.getCount(validatorID)
	return msgCount.totalMessages, msgCount.poolMessages
}

// PoolCount implements CountingTracker
func (mt *msgTracker) PoolCount() uint32 { return mt.poolCount }

// getCount returns the message count for [validatorID]
// assumes the lock is held
func (mt *msgTracker) getCount(validatorID ids.ShortID) *msgCount {
	key := validatorID.Key()
	if msgCount, exists := mt.msgSpenders[key]; exists {
		return msgCount
	}

	msgCount := &msgCount{}
	mt.msgSpenders[key] = msgCount
	return msgCount
}

type msgCount struct {
	totalMessages, poolMessages uint32
}
