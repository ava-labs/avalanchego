package blacklist

import (
	"container/list"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// If a peer consistently does not respond to queries, it will
// increase latencies on the network whenever that peer is polled.
// If we cannot terminate the poll early, then the poll will wait
// the full timeout before finalizing the poll and making progress.
// This can increase network latencies to an undesirable level.

// Therefore, a blacklist is used as a heurstic to immediately fail
// queries to nodes that are consistently not responding.

type queryBlacklist struct {
	vdrs validators.Set
	// Map of peerIDs to pending query requests
	pendingQueries map[[20]byte]map[uint32]struct{}
	// Map of consecutive query failures
	consecutiveFailures map[[20]byte]int

	// Maintain blacklist
	blacklistTimes map[[20]byte]time.Time
	blacklistOrder *list.List
	blacklistSet   ids.ShortSet

	threshold  int
	duration   time.Duration
	maxPortion float64

	clock timer.Clock

	lock sync.Mutex
}

// Config defines the configuration for a blacklist
type Config struct {
	Validators validators.Manager
	Threshold  int
	Duration   time.Duration
	MaxPortion float64
}

// Config defines the configuration for subnet specific blacklist

// NewQueryBlacklist ...
func NewQueryBlacklist(validators validators.Set, threshold int, duration time.Duration, maxPortion float64) QueryBlacklist {
	return &queryBlacklist{
		pendingQueries:      make(map[[20]byte]map[uint32]struct{}),
		consecutiveFailures: make(map[[20]byte]int),
		blacklistTimes:      make(map[[20]byte]time.Time),
		blacklistOrder:      list.New(),
		blacklistSet:        ids.ShortSet{},
		vdrs:                validators,
		threshold:           threshold,
		duration:            duration,
		maxPortion:          maxPortion,
	}
}

// QueryBlacklist ...
type QueryBlacklist interface {
	// RegisterQuery registers a sent query and returns whether the query is subject to blacklist
	RegisterQuery(ids.ShortID, uint32) bool
	// RegisterResponse registers the response to a query message
	RegisterResponse(ids.ShortID, uint32)
	// QueryFailed registers that a query did not receive a response within our synchrony bound
	QueryFailed(ids.ShortID, uint32)
}

// RegisterQuery attempts to register a query from [validatorID] and returns true
// if that request should be made (not subject to blacklisting)
func (b *queryBlacklist) RegisterQuery(validatorID ids.ShortID, requestID uint32) bool {
	b.lock.Lock()
	defer b.lock.Unlock()

	key := validatorID.Key()
	if blacklisted := b.blacklisted(validatorID); blacklisted {
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

	if ok := b.removeQuery(validatorID, requestID); !ok {
		return
	}

	// Reset consecutive failures on success
	delete(b.consecutiveFailures, validatorID.Key())
}

// QueryFailed notes a failure and blacklists [validatorID] if necessary
func (b *queryBlacklist) QueryFailed(validatorID ids.ShortID, requestID uint32) {
	b.lock.Lock()
	defer b.lock.Unlock()

	if ok := b.removeQuery(validatorID, requestID); !ok {
		return
	}

	key := validatorID.Key()
	// Add a failure and blacklist [validatorID] if it has
	// passed the threshold
	b.consecutiveFailures[key]++
	if b.consecutiveFailures[key] >= b.threshold {
		b.blacklist(validatorID)
	}
}

func (b *queryBlacklist) blacklist(validatorID ids.ShortID) {
	if b.blacklistSet.Contains(validatorID) {
		return
	}

	key := validatorID.Key()

	// Add to blacklist times
	b.blacklistTimes[key] = b.clock.Time().Add(b.duration)
	b.blacklistOrder.PushBack(validatorID)
	b.blacklistSet.Add(validatorID)

	// Note: there could be a memory leak if a large number of
	// validators were added, sampled, and then were blacklisted
	// and never sampled again. Due to the minimum staking amount
	// and durations this is not a concern.
	b.cleanup()
}

// blacklisted checks if [validatorID] is currently blacklisted
// and calls cleanup if its blacklist period has elapsed
func (b *queryBlacklist) blacklisted(validatorID ids.ShortID) bool {
	key := validatorID.Key()

	end, ok := b.blacklistTimes[key]
	if !ok {
		return false
	}

	if b.clock.Time().Before(end) {
		return true
	}

	// If a blacklisted item has expired, cleanup the blacklist
	b.cleanup()
	return false
}

// cleanup ensures that we have not blacklisted too much stake
// and removes anything from the blacklist whose time has expired
func (b *queryBlacklist) cleanup() {
	currentWeight, err := b.vdrs.SubsetWeight(b.blacklistSet)
	if err != nil {
		b.reset()
		return
	}

	maxBlacklistWeight := uint64(float64(b.vdrs.Weight()) * b.maxPortion)

	// Iterate over elements of the blacklist in order of expiration
	for e := b.blacklistOrder.Front(); e != nil; e = e.Next() {
		validatorID := e.Value.(ids.ShortID)
		// Remove elements with the next expiration until
		key := validatorID.Key()
		end := b.blacklistTimes[key]
		// Note: this creates an edge case where blacklisting a validator
		// with a sufficient stake may clear the blacklist
		if b.clock.Time().Before(end) && currentWeight < maxBlacklistWeight {
			break
		}

		removeWeight, ok := b.vdrs.GetWeight(validatorID)
		if ok {
			newWeight, err := safemath.Sub64(currentWeight, removeWeight)
			if err != nil {
				b.reset()
				return
			}
			currentWeight = newWeight
		}

		b.blacklistOrder.Remove(e)
		delete(b.blacklistTimes, key)
		b.blacklistSet.Remove(validatorID)
	}
}

func (b *queryBlacklist) reset() {
	b.pendingQueries = make(map[[20]byte]map[uint32]struct{})
	b.consecutiveFailures = make(map[[20]byte]int)
	b.blacklistTimes = make(map[[20]byte]time.Time)
	b.blacklistOrder.Init()
	b.blacklistSet.Clear()
}

// removeQuery returns true if the query was present
func (b *queryBlacklist) removeQuery(validatorID ids.ShortID, requestID uint32) bool {
	key := validatorID.Key()

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
