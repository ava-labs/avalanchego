package benchlist

import (
	"container/list"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// If a peer consistently does not respond to queries, it will
// increase latencies on the network whenever that peer is polled.
// If we cannot terminate the poll early, then the poll will wait
// the full timeout before finalizing the poll and making progress.
// This can increase network latencies to an undesirable level.

// Therefore, nodes that consistently fail are "benched" such that
// queries to that node fail immediately to avoid waiting up to
// the full network timeout for a response.

// QueryBenchlist ...
type QueryBenchlist interface {
	// RegisterQuery registers a sent query and returns whether the query is subject to benchlist
	RegisterQuery(validatorID ids.ShortID, requestID uint32) bool
	// RegisterResponse registers the response to a query message
	RegisterResponse(validatorID ids.ShortID, requstID uint32)
	// QueryFailed registers that a query did not receive a response within our synchrony bound
	QueryFailed(validatorID ids.ShortID, requestID uint32)
	// IsBenched returns true if messages to [validatorID]
	// should not be sent over the network and should immediately fail.
	IsBenched(validatorID ids.ShortID) bool
}

type queryBenchlist struct {
	lock    sync.Mutex
	ctx     *snow.Context
	metrics *metrics
	// Tells the time. Can be faked for testing.
	clock timer.Clock

	// Validator set of the network
	vdrs validators.Set
	// Validator ID --> Request ID --> non-empty iff
	// there is an outstanding request to this validator
	// with the corresponding requestID
	pendingQueries map[ids.ShortID]map[uint32]pendingQuery
	// Validator ID --> Consecutive failure information
	consecutiveFailures map[ids.ShortID]failureStreak

	// Validator ID --> Time they are benched until
	// If a validator is not benched, their ID is not a key in this map
	benchlistTimes map[ids.ShortID]time.Time
	benchlistOrder *list.List
	// IDs of validators that are currently benched
	benchlistSet ids.ShortSet

	// A validator will be benched if [threshold] messages in a row
	// to them time out and the first of those messages was more than
	// [minimumFailingDuration] ago
	threshold              int
	minimumFailingDuration time.Duration
	// A benched validator will be benched for between [duration/2] and [duration]
	duration time.Duration
	// The maximum percentage of total network stake that may be benched
	// Must be in [0,1)
	maxPortion float64
}

type pendingQuery struct {
	registered time.Time
}

type failureStreak struct {
	// Time of first consecutive timeout
	firstFailure time.Time
	// Number of consecutive message timeouts
	consecutive int
}

// NewQueryBenchlist ...
func NewQueryBenchlist(
	validators validators.Set,
	ctx *snow.Context,
	threshold int,
	minimumFailingDuration,
	duration time.Duration,
	maxPortion float64,
	namespace string,
) (QueryBenchlist, error) {
	if maxPortion < 0 || maxPortion >= 1 {
		return nil, fmt.Errorf("max portion of benched stake must be in [0,1) but got %f", maxPortion)
	}
	metrics := &metrics{}
	return &queryBenchlist{
		pendingQueries:         make(map[ids.ShortID]map[uint32]pendingQuery),
		consecutiveFailures:    make(map[ids.ShortID]failureStreak),
		benchlistTimes:         make(map[ids.ShortID]time.Time),
		benchlistOrder:         list.New(),
		benchlistSet:           ids.ShortSet{},
		vdrs:                   validators,
		threshold:              threshold,
		minimumFailingDuration: minimumFailingDuration,
		duration:               duration,
		maxPortion:             maxPortion,
		ctx:                    ctx,
		metrics:                metrics,
	}, metrics.Initialize(ctx, namespace)
}

// IsBenched returns true if messages to [validatorID]
// should not be sent over the network and should immediately fail.
func (b *queryBenchlist) IsBenched(validatorID ids.ShortID) bool {
	b.lock.Lock()
	isBenched := b.isBenched(validatorID)
	b.lock.Unlock()
	return isBenched
}

// isBenched checks if [validatorID] is currently benched
// and calls cleanup if its benching period has elapsed
func (b *queryBenchlist) isBenched(validatorID ids.ShortID) bool {
	end, ok := b.benchlistTimes[validatorID]
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

// RegisterQuery attempts to register a query from [validatorID] and returns true
// if that request should be made (not subject to benchlisting)
func (b *queryBenchlist) RegisterQuery(validatorID ids.ShortID, requestID uint32) bool {
	b.lock.Lock()
	defer b.lock.Unlock()

	if benched := b.isBenched(validatorID); benched {
		return false
	}

	validatorRequests, ok := b.pendingQueries[validatorID]
	if !ok {
		validatorRequests = make(map[uint32]pendingQuery)
		b.pendingQueries[validatorID] = validatorRequests
	}

	validatorRequests[requestID] = pendingQuery{
		registered: b.clock.Time(),
	}
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
	delete(b.consecutiveFailures, validatorID)
}

// QueryFailed notes a failure and benchlists [validatorID] if necessary
func (b *queryBenchlist) QueryFailed(validatorID ids.ShortID, requestID uint32) {
	b.lock.Lock()
	defer b.lock.Unlock()

	if ok := b.removeQuery(validatorID, requestID); !ok {
		return
	}

	// Track the message failure and bench [validatorID] if it has
	// surpassed the threshold
	currentTime := b.clock.Time()
	failureStreak := b.consecutiveFailures[validatorID]
	if failureStreak.consecutive == 0 {
		failureStreak.firstFailure = currentTime
	}
	failureStreak.consecutive++
	b.consecutiveFailures[validatorID] = failureStreak

	if failureStreak.consecutive >= b.threshold && !currentTime.Before(failureStreak.firstFailure.Add(b.minimumFailingDuration)) {
		b.bench(validatorID)
	}
}

func (b *queryBenchlist) bench(validatorID ids.ShortID) {
	if b.benchlistSet.Contains(validatorID) {
		return
	}

	// Goal:
	// Random end time in the range:
	// [max(lastEndTime, (currentTime + (duration/2)): currentTime + duration]
	// This maintains the invariant that validators in benchlistOrder are
	// ordered by the time that they should be unbenched
	currTime := b.clock.Time()
	minEndTime := currTime.Add(b.duration / 2)
	if elem := b.benchlistOrder.Back(); elem != nil {
		lastValidator := elem.Value.(ids.ShortID)
		lastEndTime := b.benchlistTimes[lastValidator]
		if lastEndTime.After(minEndTime) {
			minEndTime = lastEndTime
		}
	}
	maxEndTime := currTime.Add(b.duration)
	// Since maxEndTime is at least [duration] in the future and every element
	// added to benchlist was added in the past with an end time at most [duration]
	// in the future, this should never produce a negative duration.
	diff := maxEndTime.Sub(minEndTime)
	randomizedEndTime := minEndTime.Add(time.Duration(rand.Float64() * float64(diff))) // #nosec G404

	// Add to benchlist times with randomized delay
	b.benchlistTimes[validatorID] = randomizedEndTime
	b.benchlistOrder.PushBack(validatorID)
	b.benchlistSet.Add(validatorID)
	delete(b.consecutiveFailures, validatorID)
	b.ctx.Log.Debug(
		"benching validator %s after %d consecutive failed queries for %s",
		validatorID,
		b.threshold,
		randomizedEndTime.Sub(currTime),
	)

	// Note: there could be a memory leak if a large number of
	// validators were added, sampled, benched, and never sampled
	// again. Due to the minimum staking amount and durations this
	// is not a realistic concern.
	b.cleanup()
}

// cleanup ensures that we have not benched too much stake
// and removes anything from the benchlist whose time has expired
func (b *queryBenchlist) cleanup() {
	currentWeight, err := b.vdrs.SubsetWeight(b.benchlistSet)
	if err != nil {
		// Add log for this, should never happen
		b.ctx.Log.Error("failed to calculate subset weight due to: %w. Resetting benchlist", err)
		b.reset()
		return
	}

	currentTime := b.clock.Time()

	benchLen := b.benchlistSet.Len()
	updatedWeight := currentWeight
	totalWeight := b.vdrs.Weight()
	maxBenchlistWeight := uint64(float64(totalWeight) * b.maxPortion)
	// Iterate over elements of the benchlist in order of expiration
	for b.benchlistOrder.Len() > 0 {
		e := b.benchlistOrder.Front()

		validatorID := e.Value.(ids.ShortID)
		end := b.benchlistTimes[validatorID]
		// Remove elements with the next expiration until the next item has not
		// expired and the bench has less than the maximum weight
		// Note: this creates an edge case where benching a validator
		// with a sufficient stake may clear the bench if the benchlist is
		// not parameterized correctly.
		if currentTime.Before(end) && updatedWeight < maxBenchlistWeight {
			break
		}

		removeWeight, ok := b.vdrs.GetWeight(validatorID)
		if ok {
			newWeight, err := safemath.Sub64(updatedWeight, removeWeight)
			if err != nil {
				b.ctx.Log.Error("failed to calculate new subset weight due to: %w. Resetting benchlist", err)
				b.reset()
				return
			}
			updatedWeight = newWeight
		}

		b.ctx.Log.Debug("Removed Validator: (%s, %d). EndTime: %s. CurrentTime: %s)", validatorID, removeWeight, end, currentTime)
		b.benchlistOrder.Remove(e)
		delete(b.benchlistTimes, validatorID)
		b.benchlistSet.Remove(validatorID)
	}

	updatedBenchLen := b.benchlistSet.Len()
	b.ctx.Log.Debug("Maximum Benchable Weight: %d. Benched Weight: (%d/%d) -> (%d/%d). Benched Validators: %d -> %d.",
		maxBenchlistWeight,
		currentWeight,
		totalWeight,
		updatedWeight,
		totalWeight,
		benchLen,
		updatedBenchLen,
	)
	b.metrics.weightBenched.Set(float64(updatedWeight))
	b.metrics.numBenched.Set(float64(updatedBenchLen))
}

func (b *queryBenchlist) reset() {
	b.pendingQueries = make(map[ids.ShortID]map[uint32]pendingQuery)
	b.consecutiveFailures = make(map[ids.ShortID]failureStreak)
	b.benchlistTimes = make(map[ids.ShortID]time.Time)
	b.benchlistOrder.Init()
	b.benchlistSet.Clear()
	b.metrics.weightBenched.Set(0)
	b.metrics.numBenched.Set(0)
}

// removeQuery returns true if the query was present
func (b *queryBenchlist) removeQuery(validatorID ids.ShortID, requestID uint32) bool {
	validatorRequests, ok := b.pendingQueries[validatorID]
	if !ok {
		return false
	}

	_, ok = validatorRequests[requestID]
	if !ok {
		return false
	}

	delete(validatorRequests, requestID)
	if len(validatorRequests) == 0 {
		delete(b.pendingQueries, validatorID)
	}
	return true
}
