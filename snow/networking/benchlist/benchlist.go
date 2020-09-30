package benchlist

import (
	"container/list"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// QueryBenchlist ...
type QueryBenchlist interface {
	// RegisterQuery registers a sent query and returns whether the query is subject to benchlist
	RegisterQuery(ids.ShortID, uint32) bool
	// RegisterResponse registers the response to a query message
	RegisterResponse(ids.ShortID, uint32)
	// QueryFailed registers that a query did not receive a response within our synchrony bound
	QueryFailed(ids.ShortID, uint32)
}

// If a peer consistently does not respond to queries, it will
// increase latencies on the network whenever that peer is polled.
// If we cannot terminate the poll early, then the poll will wait
// the full timeout before finalizing the poll and making progress.
// This can increase network latencies to an undesirable level.

// Therefore, a benchlist is used as a heurstic to immediately fail
// queries to nodes that are consistently not responding.

type queryBenchlist struct {
	vdrs validators.Set
	// Validator ID --> Request ID --> non-empty iff
	// there is an outstanding request to this validator
	// with the corresponding requestID
	pendingQueries map[[20]byte]map[uint32]struct{}
	// Map of consecutive query failures
	consecutiveFailures map[[20]byte]int

	// Maintain benchlist
	benchlistTimes map[[20]byte]time.Time
	benchlistOrder *list.List
	benchlistSet   ids.ShortSet

	threshold  int
	duration   time.Duration
	maxPortion float64

	clock timer.Clock

	lock sync.Mutex
}

// Config defines the configuration for a benchlist
type Config struct {
	Validators validators.Manager
	Threshold  int
	Duration   time.Duration
	MaxPortion float64
}

// NewQueryBenchlist ...
func NewQueryBenchlist(validators validators.Set, threshold int, duration time.Duration, maxPortion float64) QueryBenchlist {
	return &queryBenchlist{
		pendingQueries:      make(map[[20]byte]map[uint32]struct{}),
		consecutiveFailures: make(map[[20]byte]int),
		benchlistTimes:      make(map[[20]byte]time.Time),
		benchlistOrder:      list.New(),
		benchlistSet:        ids.ShortSet{},
		vdrs:                validators,
		threshold:           threshold,
		duration:            duration,
		maxPortion:          maxPortion,
	}
}

// RegisterQuery attempts to register a query from [validatorID] and returns true
// if that request should be made (not subject to benchlisting)
func (b *queryBenchlist) RegisterQuery(validatorID ids.ShortID, requestID uint32) bool {
	b.lock.Lock()
	defer b.lock.Unlock()

	key := validatorID.Key()
	if benchlisted := b.benchlisted(validatorID); benchlisted {
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
func (b *queryBenchlist) RegisterResponse(validatorID ids.ShortID, requestID uint32) {
	b.lock.Lock()
	defer b.lock.Unlock()

	if ok := b.removeQuery(validatorID, requestID); !ok {
		return
	}

	// Reset consecutive failures on success
	delete(b.consecutiveFailures, validatorID.Key())
}

// QueryFailed notes a failure and benchlists [validatorID] if necessary
func (b *queryBenchlist) QueryFailed(validatorID ids.ShortID, requestID uint32) {
	b.lock.Lock()
	defer b.lock.Unlock()

	if ok := b.removeQuery(validatorID, requestID); !ok {
		return
	}

	key := validatorID.Key()
	// Add a failure and benchlist [validatorID] if it has
	// passed the threshold
	b.consecutiveFailures[key]++
	if b.consecutiveFailures[key] >= b.threshold {
		b.benchlist(validatorID)
	}
}

func (b *queryBenchlist) benchlist(validatorID ids.ShortID) {
	if b.benchlistSet.Contains(validatorID) {
		return
	}

	key := validatorID.Key()

	// Add to benchlist times
	// Note: we do not randomize the delay because nodes carry out
	// a unique local view of when peers have surpassed the threshold
	// of failed messages. Therefore, it's highly unlikely that all
	// nodes will unbench a node at the same time.
	// Additionally, if a node is unbenched simultaneously by the entire
	// network, this only means that it will begin to receive consensus
	// queries at the normal rate. There will be no backlog of messages
	// that could spam an unbenched node.
	b.benchlistTimes[key] = b.clock.Time().Add(b.duration)
	b.benchlistOrder.PushBack(validatorID)
	b.benchlistSet.Add(validatorID)
	delete(b.consecutiveFailures, key)

	// Note: there could be a memory leak if a large number of
	// validators were added, sampled, benched, and never sampled
	// again. Due to the minimum staking amount and durations this
	// is not a realistic concern.
	b.cleanup()
}

// benchlisted checks if [validatorID] is currently benchlisted
// and calls cleanup if its benchlist period has elapsed
func (b *queryBenchlist) benchlisted(validatorID ids.ShortID) bool {
	key := validatorID.Key()

	end, ok := b.benchlistTimes[key]
	if !ok {
		return false
	}

	if b.clock.Time().Before(end) {
		return true
	}

	// If a benchlisted item has expired, cleanup the benchlist
	b.cleanup()
	return false
}

// cleanup ensures that we have not benchlisted too much stake
// and removes anything from the benchlist whose time has expired
func (b *queryBenchlist) cleanup() {
	currentWeight, err := b.vdrs.SubsetWeight(b.benchlistSet)
	if err != nil {
		// Add log for this, should never happen
		b.reset()
		return
	}

	maxBenchlistWeight := uint64(float64(b.vdrs.Weight()) * b.maxPortion)

	// Iterate over elements of the benchlist in order of expiration
	for e := b.benchlistOrder.Front(); e != nil; e = e.Next() {
		validatorID := e.Value.(ids.ShortID)
		// Remove elements with the next expiration until
		key := validatorID.Key()
		end := b.benchlistTimes[key]
		// Note: this creates an edge case where benchlisting a validator
		// with a sufficient stake may clear the benchlist
		if b.clock.Time().Before(end) && currentWeight < maxBenchlistWeight {
			break
		}

		removeWeight, ok := b.vdrs.GetWeight(validatorID)
		if ok {
			newWeight, err := safemath.Sub64(currentWeight, removeWeight)
			if err != nil {
				// Add log for this, potentially add internal validators set
				// to maintain weights
				b.reset()
				return
			}
			currentWeight = newWeight
		}

		b.benchlistOrder.Remove(e)
		delete(b.benchlistTimes, key)
		b.benchlistSet.Remove(validatorID)
	}
}

func (b *queryBenchlist) reset() {
	b.pendingQueries = make(map[[20]byte]map[uint32]struct{})
	b.consecutiveFailures = make(map[[20]byte]int)
	b.benchlistTimes = make(map[[20]byte]time.Time)
	b.benchlistOrder.Init()
	b.benchlistSet.Clear()
}

// removeQuery returns true if the query was present
func (b *queryBenchlist) removeQuery(validatorID ids.ShortID, requestID uint32) bool {
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
