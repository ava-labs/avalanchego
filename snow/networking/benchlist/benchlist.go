package benchlist

import (
	"container/list"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/prometheus/client_golang/prometheus"

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

// Benchlist ...
type Benchlist interface {
	// RegisterResponse registers the response to a query message
	RegisterResponse(validatorID ids.ShortID)
	// RegisterFailure registers that we didn't receive a response within the timeout
	RegisterFailure(validatorID ids.ShortID)
	// IsBenched returns true if messages to [validatorID]
	// should not be sent over the network and should immediately fail.
	IsBenched(validatorID ids.ShortID) bool
}

type benchlist struct {
	lock    sync.RWMutex
	log     logging.Logger
	metrics metrics
	// Tells the time. Can be faked for testing.
	clock timer.Clock

	// Validator set of the network
	vdrs validators.Set
	// Validator ID --> Consecutive failure information
	failureStreaks map[ids.ShortID]failureStreak

	// Validator ID --> Time they are benched until
	// If a validator is not benched, their ID is not a key in this map
	benchlistTimes map[ids.ShortID]time.Time
	// TODO make this a heap instead of a list
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

type failureStreak struct {
	// Time of first consecutive timeout
	firstFailure time.Time
	// Number of consecutive message timeouts
	consecutive int
}

// NewBenchlist ...
func NewBenchlist(
	log logging.Logger,
	validators validators.Set,
	threshold int,
	minimumFailingDuration,
	duration time.Duration,
	maxPortion float64,
	namespace string,
	registerer prometheus.Registerer,
) (Benchlist, error) {
	if maxPortion < 0 || maxPortion >= 1 {
		return nil, fmt.Errorf("max portion of benched stake must be in [0,1) but got %f", maxPortion)
	}
	benchlist := &benchlist{
		failureStreaks:         make(map[ids.ShortID]failureStreak),
		benchlistTimes:         make(map[ids.ShortID]time.Time),
		benchlistOrder:         list.New(),
		benchlistSet:           ids.ShortSet{},
		vdrs:                   validators,
		threshold:              threshold,
		minimumFailingDuration: minimumFailingDuration,
		duration:               duration,
		maxPortion:             maxPortion,
	}
	return benchlist, benchlist.metrics.Initialize(registerer, namespace)
}

// IsBenched returns true if messages to [validatorID]
// should not be sent over the network and should immediately fail.
func (b *benchlist) IsBenched(validatorID ids.ShortID) bool {
	b.lock.RLock()
	isBenched := b.isBenched(validatorID)
	b.lock.RUnlock()
	return isBenched
}

// isBenched checks if [validatorID] is currently benched
// and calls cleanup if its benching period has elapsed
func (b *benchlist) isBenched(validatorID ids.ShortID) bool {
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

// RegisterResponse notes that we received a response from validator [validatorID]
func (b *benchlist) RegisterResponse(validatorID ids.ShortID) {
	b.lock.Lock()
	delete(b.failureStreaks, validatorID)
	b.lock.Unlock()
}

// RegisterResponse notes that a request to validator [validatorID] timed out
func (b *benchlist) RegisterFailure(validatorID ids.ShortID) {
	b.lock.Lock()
	failureStreak := b.failureStreaks[validatorID]
	failureStreak.consecutive++
	if failureStreak.firstFailure.IsZero() {
		// This is the first consecutive failure
		failureStreak.firstFailure = b.clock.Time()
	}
	b.failureStreaks[validatorID] = failureStreak

	now := b.clock.Time()
	if failureStreak.consecutive >= b.threshold && now.After(failureStreak.firstFailure.Add(b.minimumFailingDuration)) {
		b.bench(validatorID)
	}

	b.lock.Unlock()
}

// Assumes [b.lock] is held
func (b *benchlist) bench(validatorID ids.ShortID) {
	if b.benchlistSet.Contains(validatorID) {
		return
	}

	// Goal:
	// Random end time in the range:
	// [max(lastEndTime, (currentTime + (duration/2)): currentTime + duration]
	// This maintains the invariant that validators in benchlistOrder are
	// ordered by the time that they should be unbenched
	now := b.clock.Time()
	minEndTime := now.Add(b.duration / 2)
	if elem := b.benchlistOrder.Back(); elem != nil {
		lastValidator := elem.Value.(ids.ShortID)
		lastEndTime := b.benchlistTimes[lastValidator]
		if lastEndTime.After(minEndTime) {
			minEndTime = lastEndTime
		}
	}
	maxEndTime := now.Add(b.duration)
	// Since maxEndTime is at least [duration] in the future and every element
	// added to benchlist was added in the past with an end time at most [duration]
	// in the future, this should never produce a negative duration.
	diff := maxEndTime.Sub(minEndTime)
	randomizedEndTime := minEndTime.Add(time.Duration(rand.Float64() * float64(diff))) // #nosec G404

	// Add to benchlist times with randomized delay
	b.benchlistTimes[validatorID] = randomizedEndTime
	b.benchlistOrder.PushBack(validatorID)
	b.benchlistSet.Add(validatorID)
	delete(b.failureStreaks, validatorID)
	b.log.Debug(
		"benching validator %s after %d consecutive failed queries for %s",
		validatorID,
		b.threshold,
		randomizedEndTime.Sub(now),
	)

	// Note: there could be a memory leak if a large number of
	// validators were added, sampled, benched, and never sampled
	// again. Due to the minimum staking amount and durations this
	// is not a realistic concern.
	b.cleanup()
}

// cleanup ensures that we have not benched too much stake
// and removes anything from the benchlist whose time has expired
// Assumes [b.lock] is held
func (b *benchlist) cleanup() {
	currentWeight, err := b.vdrs.SubsetWeight(b.benchlistSet)
	if err != nil {
		// Add log for this, should never happen
		b.log.Error("failed to calculate subset weight due to: %w. Resetting benchlist", err)
		b.reset()
		return
	}

	now := b.clock.Time()

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
		if now.Before(end) && updatedWeight < maxBenchlistWeight {
			break
		}

		removeWeight, ok := b.vdrs.GetWeight(validatorID)
		if ok {
			updatedWeight, err = safemath.Sub64(updatedWeight, removeWeight)
			if err != nil {
				b.log.Error("failed to calculate new subset weight due to: %w. Resetting benchlist", err)
				b.reset()
				return
			}
		}

		b.log.Debug("Removed Validator: (%s, %d). EndTime: %s. CurrentTime: %s)", validatorID, removeWeight, end, now)
		b.benchlistOrder.Remove(e)
		delete(b.benchlistTimes, validatorID)
		b.benchlistSet.Remove(validatorID)
	}

	updatedBenchLen := b.benchlistSet.Len()
	b.log.Debug("Maximum Benchable Weight: %d. Benched Weight: (%d/%d) -> (%d/%d). Benched Validators: %d -> %d.",
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

func (b *benchlist) reset() {
	b.failureStreaks = make(map[ids.ShortID]failureStreak)
	b.benchlistTimes = make(map[ids.ShortID]time.Time)
	b.benchlistOrder.Init()
	b.benchlistSet.Clear()
	b.metrics.weightBenched.Set(0)
	b.metrics.numBenched.Set(0)
}
