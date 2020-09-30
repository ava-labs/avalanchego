package benchlist

import (
	"container/list"
	"math/rand"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
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

	threshold    int
	halfDuration time.Duration
	maxPortion   float64

	clock timer.Clock

	metrics *metrics
	ctx     *snow.Context

	lock sync.Mutex
}

// NewQueryBenchlist ...
func NewQueryBenchlist(validators validators.Set, ctx *snow.Context, threshold int, duration time.Duration, maxPortion float64) QueryBenchlist {
	metrics := &metrics{}
	metrics.Initialize(ctx.Namespace, ctx.Metrics)

	return &queryBenchlist{
		pendingQueries:      make(map[[20]byte]map[uint32]struct{}),
		consecutiveFailures: make(map[[20]byte]int),
		benchlistTimes:      make(map[[20]byte]time.Time),
		benchlistOrder:      list.New(),
		benchlistSet:        ids.ShortSet{},
		vdrs:                validators,
		threshold:           threshold,
		halfDuration:        duration / 2,
		maxPortion:          maxPortion,
		ctx:                 ctx,
		metrics:             metrics,
	}
}

// RegisterQuery attempts to register a query from [validatorID] and returns true
// if that request should be made (not subject to benchlisting)
func (b *queryBenchlist) RegisterQuery(validatorID ids.ShortID, requestID uint32) bool {
	b.lock.Lock()
	defer b.lock.Unlock()

	key := validatorID.Key()
	if benched := b.benched(validatorID); benched {
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
	// Add a failure and benches [validatorID] if it has
	// passed the threshold
	b.consecutiveFailures[key]++
	if b.consecutiveFailures[key] >= b.threshold {
		b.bench(validatorID)
	}
}

func (b *queryBenchlist) bench(validatorID ids.ShortID) {
	if b.benchlistSet.Contains(validatorID) {
		return
	}

	key := validatorID.Key()

	// Add to benchlist times with randomized delay
	randomizedDuration := time.Duration(rand.Float64()*float64(b.halfDuration)) + b.halfDuration // #nosec G404
	b.benchlistTimes[key] = b.clock.Time().Add(randomizedDuration)
	b.benchlistOrder.PushBack(validatorID)
	b.benchlistSet.Add(validatorID)
	delete(b.consecutiveFailures, key)
	b.metrics.numBenched.Inc()
	b.ctx.Log.Debug("Benching validator %s for %v after %d consecutive failed queries", validatorID, randomizedDuration, b.threshold)

	// Note: there could be a memory leak if a large number of
	// validators were added, sampled, benched, and never sampled
	// again. Due to the minimum staking amount and durations this
	// is not a realistic concern.
	b.cleanup()
}

// benched checks if [validatorID] is currently benched
// and calls cleanup if its benching period has elapsed
func (b *queryBenchlist) benched(validatorID ids.ShortID) bool {
	key := validatorID.Key()

	end, ok := b.benchlistTimes[key]
	if !ok {
		return false
	}

	if b.clock.Time().Before(end) {
		return true
	}

	// If a benched item has expired, cleanup the benchlist
	b.cleanup()
	return false
}

// cleanup ensures that we have not benched too much stake
// and removes anything from the benchlist whose time has expired
func (b *queryBenchlist) cleanup() {
	currentWeight, err := b.vdrs.SubsetWeight(b.benchlistSet)
	if err != nil {
		// Add log for this, should never happen
		b.ctx.Log.Error("Failed to calculate subset weight due to: %w. Resetting benchlist.", err)
		b.reset()
		return
	}

	numBenched := b.benchlistSet.Len()
	updatedWeight := currentWeight
	totalWeight := b.vdrs.Weight()
	maxBenchlistWeight := uint64(float64(totalWeight) * b.maxPortion)

	// Iterate over elements of the benchlist in order of expiration
	for e := b.benchlistOrder.Front(); e != nil; e = e.Next() {
		validatorID := e.Value.(ids.ShortID)
		key := validatorID.Key()
		end := b.benchlistTimes[key]
		// Remove elements with the next expiration until the next item has not
		// expired and the bench has less than the maximum weight
		// Note: this creates an edge case where benchlisting a validator
		// with a sufficient stake may clear the benchlist
		if b.clock.Time().Before(end) && currentWeight < maxBenchlistWeight {
			break
		}

		removeWeight, ok := b.vdrs.GetWeight(validatorID)
		if ok {
			newWeight, err := safemath.Sub64(currentWeight, removeWeight)
			if err != nil {
				b.ctx.Log.Error("Failed to calculate new subset weight due to: %w. Resetting benchlist.", err)
				b.reset()
				return
			}
			updatedWeight = newWeight
		}

		b.benchlistOrder.Remove(e)
		delete(b.benchlistTimes, key)
		b.benchlistSet.Remove(validatorID)
		b.metrics.numBenched.Dec()
	}

	b.ctx.Log.Debug("Benchlist weight: (%v/%v) -> (%v/%v). Benched Validators: %d -> %d",
		currentWeight,
		totalWeight,
		updatedWeight,
		totalWeight,
		numBenched,
		b.benchlistSet.Len(),
	)
	b.metrics.weightBenched.Set(float64(updatedWeight))
}

func (b *queryBenchlist) reset() {
	b.pendingQueries = make(map[[20]byte]map[uint32]struct{})
	b.consecutiveFailures = make(map[[20]byte]int)
	b.benchlistTimes = make(map[[20]byte]time.Time)
	b.benchlistOrder.Init()
	b.benchlistSet.Clear()
	b.metrics.weightBenched.Set(0)
	b.metrics.numBenched.Set(0)
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
