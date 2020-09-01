// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracker

import (
	"sync"

	"github.com/ava-labs/gecko/ids"
)

// msgTracker implements CountingTracker to keep track of pending messages to peers
type msgTracker struct {
	lock sync.Mutex

	// Track peers outstanding messages
	msgSpenders map[[20]byte]uint32
}

// NewMessageTracker returns a CountingTracker to track
// pending messages from peers
func NewMessageTracker() CountingTracker {
	return &msgTracker{
		msgSpenders: make(map[[20]byte]uint32),
	}
}

// Add implements CountingTracker
func (et *msgTracker) Add(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	et.msgSpenders[validatorID.Key()]++

}

// Remove implements CountingTracker
func (et *msgTracker) Remove(validatorID ids.ShortID) {
	et.lock.Lock()
	defer et.lock.Unlock()

	key := validatorID.Key()
	et.msgSpenders[key]--
	if et.msgSpenders[key] == 0 {
		delete(et.msgSpenders, key)
	}
}

// OutstandingCount implements CountingTracker
func (et *msgTracker) OutstandingCount(validatorID ids.ShortID) uint32 {
	et.lock.Lock()
	defer et.lock.Unlock()

	return et.msgSpenders[validatorID.Key()]
}
