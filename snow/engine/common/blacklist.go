package common

import (
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/timer"
)

// Problem: register queries and the responses
// to ensure that nodes are responding to messages

// If a peer consistently does not respond to queries, it will
// increase latencies on the network whenever that peer is polled.
// If we cannot terminate the poll early, then the poll will wait
// the full timeout before finalizing the poll and making progress.
// This can increase network latencies to an undesirable level.

// Therefore, a blacklist is used as a heurstic to immediately fail
// queries to nodes that are consistently not responding.

type queryBlacklist struct {
	// Map of peerIDs to pending query requests
	pendingQueries map[[20]byte]map[uint32]struct{}
	// Map of peerIDs to the time that they are blacklisted until
	blacklist map[[20]byte]time.Time

	// Map of consecutive query failures
	consecutiveFailures map[[20]byte]int

	threshold int
	duration  time.Duration

	clock timer.Clock

	lock sync.Mutex
}

// NewQueryBlacklist ...
func NewQueryBlacklist(threshold int, duration time.Duration) Blacklist {
	return &queryBlacklist{
		pendingQueries:      make(map[[20]byte]map[uint32]struct{}),
		blacklist:           make(map[[20]byte]time.Time),
		consecutiveFailures: make(map[[20]byte]int),
		threshold:           threshold,
		duration:            duration,
	}
}

// Blacklist ...
type Blacklist interface {
	// RegisterQuery registers a sent query and returns whether the query is subject to blacklist
	RegisterQuery(ids.ShortID, uint32) bool
	// RegisterResponse registers the response to a query message
	RegisterResponse(ids.ShortID, uint32)
	// QueryFailed registers that a query did not receive a response within our synchrony bound
	QueryFailed(ids.ShortID, uint32)
}

// RegisterQuery attempts to register a query from [validatorID] and returns true
// if that request is not subject to the blacklist
func (b *queryBlacklist) RegisterQuery(validatorID ids.ShortID, requestID uint32) bool {
	b.lock.Lock()
	defer b.lock.Unlock()

	key := validatorID.Key()
	if blacklisted := b.blacklisted(key); blacklisted {
		return false
	}
	validatorRequests, ok := b.pendingQueries[key]
	if !ok {
		validatorRequests = make(map[uint32]struct{})
		b.pendingQueries[key] = validatorRequests
	}
	validatorRequests[requestID] = struct{}{}

	return true
}

// RegisterResponse removes the query from pending
func (b *queryBlacklist) RegisterResponse(validatorID ids.ShortID, requestID uint32) {
	b.lock.Lock()
	defer b.lock.Unlock()

	key := validatorID.Key()
	if ok := b.removeQuery(key, requestID); !ok {
		return
	}
	// Reset consecutive failures on success
	delete(b.consecutiveFailures, key)
}

// QueryFailed notes a failure and adds [validatorID] to the blacklist if necessary
func (b *queryBlacklist) QueryFailed(validatorID ids.ShortID, requestID uint32) {
	b.lock.Lock()
	defer b.lock.Unlock()

	key := validatorID.Key()
	if ok := b.removeQuery(key, requestID); !ok {
		return
	}
	// Add a failure and blacklist [validatorID] if it has
	// passed the threshold
	b.consecutiveFailures[key]++
	if b.consecutiveFailures[key] >= b.threshold {
		// Remove consecutive failures and add to blacklist
		delete(b.consecutiveFailures, key)
		b.blacklist[key] = b.clock.Time().Add(b.duration)
	}
}

func (b *queryBlacklist) blacklisted(key [20]byte) bool {
	until, ok := b.blacklist[key]
	if !ok {
		return false
	}

	if b.clock.Time().Before(until) {
		return true
	}
	// Note: this currently causes a memory leak since the blacklist will only be
	// cleaned up on subsequent queries
	// For now, blacklisted items will be monotonic, so we can track what to delete via a simple FIFO
	// queue, peek the element at the head and delete until we get to an element
	// that still belongs on the blacklist

	// Remove from the blacklist if its time has expired
	delete(b.blacklist, key)
	return false
}

// removeQuery returns true if the query was present
func (b *queryBlacklist) removeQuery(key [20]byte, requestID uint32) bool {
	validatorRequests, ok := b.pendingQueries[key]
	if !ok {
		return false
	}

	_, ok = validatorRequests[requestID]
	if ok {
		delete(validatorRequests, requestID)
		if len(validatorRequests) == 0 {
			delete(b.pendingQueries, key)
		}
	}
	return ok
}

type noBlacklist struct{}

// NewNoBlacklist returns an empty blacklist that will never stop any queries
func NewNoBlacklist() Blacklist { return &noBlacklist{} }

// RegisterQuery ...
func (b *noBlacklist) RegisterQuery(validatorID ids.ShortID, requestID uint32) bool { return true }

// RegisterResponse ...
func (b *noBlacklist) RegisterResponse(validatorID ids.ShortID, requestID uint32) {}

// QueryFailed ...
func (b *noBlacklist) QueryFailed(validatorID ids.ShortID, requestID uint32) {}
