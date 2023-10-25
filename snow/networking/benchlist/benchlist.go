// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"

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
type Benchlist interface {
	// RegisterResponse registers the response to a query message
	RegisterResponse(nodeID ids.NodeID)
	// RegisterFailure registers that we didn't receive a response within the timeout
	RegisterFailure(nodeID ids.NodeID)
	// IsBenched returns true if messages to [validatorID]
	// should not be sent over the network and should immediately fail.
	IsBenched(nodeID ids.NodeID) bool
}

type failureStreak struct {
	// Time of first consecutive timeout
	firstFailure time.Time
	// Number of consecutive message timeouts
	consecutive int
}

type benchlist struct {
	lock sync.RWMutex
	// Context of the chain this is the benchlist for
	ctx     *snow.ConsensusContext
	metrics metrics

	// Fires when the next validator should leave the bench
	// Calls [update] when it fires
	timer *timer.Timer

	// Tells the time. Can be faked for testing.
	clock mockable.Clock

	// notified when a node is benched or unbenched
	benchable Benchable

	// Validator set of the network
	vdrs validators.Manager

	// Validator ID --> Consecutive failure information
	// [streaklock] must be held when touching [failureStreaks]
	streaklock     sync.Mutex
	failureStreaks map[ids.NodeID]failureStreak

	// IDs of validators that are currently benched
	benchlistSet set.Set[ids.NodeID]

	// Min heap of benched validators ordered by when they can be unbenched
	benchedHeap heap.Map[ids.NodeID, time.Time]

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

// NewBenchlist returns a new Benchlist
func NewBenchlist(
	ctx *snow.ConsensusContext,
	benchable Benchable,
	validators validators.Manager,
	threshold int,
	minimumFailingDuration,
	duration time.Duration,
	maxPortion float64,
) (Benchlist, error) {
	if maxPortion < 0 || maxPortion >= 1 {
		return nil, fmt.Errorf("max portion of benched stake must be in [0,1) but got %f", maxPortion)
	}
	benchlist := &benchlist{
		ctx:                    ctx,
		failureStreaks:         make(map[ids.NodeID]failureStreak),
		benchlistSet:           set.Set[ids.NodeID]{},
		benchable:              benchable,
		benchedHeap:            heap.NewMap[ids.NodeID, time.Time](time.Time.Before),
		vdrs:                   validators,
		threshold:              threshold,
		minimumFailingDuration: minimumFailingDuration,
		duration:               duration,
		maxPortion:             maxPortion,
	}
	benchlist.timer = timer.NewTimer(benchlist.update)
	go benchlist.timer.Dispatch()
	return benchlist, benchlist.metrics.Initialize(ctx.Registerer)
}

// Update removes benched validators whose time on the bench is over
func (b *benchlist) update() {
	b.lock.Lock()
	defer b.lock.Unlock()

	now := b.clock.Time()
	for {
		if !b.canUnbench(now) {
			break
		}
		b.remove()
	}
	// Set next time update will be called
	b.setNextLeaveTime()
}

// Removes the next node from the benchlist
// Assumes [b.lock] is held
func (b *benchlist) remove() {
	nodeID, _, _ := b.benchedHeap.Pop()
	b.ctx.Log.Debug("removing node from benchlist",
		zap.Stringer("nodeID", nodeID),
	)
	b.benchlistSet.Remove(nodeID)
	b.benchable.Unbenched(b.ctx.ChainID, nodeID)

	// Update metrics
	b.metrics.numBenched.Set(float64(b.benchedHeap.Len()))
	benchedStake, err := b.vdrs.SubsetWeight(b.ctx.SubnetID, b.benchlistSet)
	if err != nil {
		b.ctx.Log.Error("error calculating benched stake",
			zap.Stringer("subnetID", b.ctx.SubnetID),
			zap.Error(err),
		)
		return
	}
	b.metrics.weightBenched.Set(float64(benchedStake))
}

// Returns if a validator should leave the bench at time [now].
// False if no validator should.
// Assumes [b.lock] is held
func (b *benchlist) canUnbench(now time.Time) bool {
	_, next, ok := b.benchedHeap.Peek()
	if !ok {
		return false
	}
	return now.After(next)
}

// Set [b.timer] to fire when the next validator should leave the bench
// Assumes [b.lock] is held
func (b *benchlist) setNextLeaveTime() {
	_, next, ok := b.benchedHeap.Peek()
	if !ok {
		b.timer.Cancel()
		return
	}
	now := b.clock.Time()
	nextLeave := next.Sub(now)
	b.timer.SetTimeoutIn(nextLeave)
}

// IsBenched returns true if messages to [nodeID]
// should not be sent over the network and should immediately fail.
func (b *benchlist) IsBenched(nodeID ids.NodeID) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.isBenched(nodeID)
}

// isBenched checks if [nodeID] is currently benched
// and calls cleanup if its benching period has elapsed
// Assumes [b.lock] is held.
func (b *benchlist) isBenched(nodeID ids.NodeID) bool {
	if _, ok := b.benchlistSet[nodeID]; ok {
		return true
	}
	return false
}

// RegisterResponse notes that we received a response from validator [validatorID]
func (b *benchlist) RegisterResponse(nodeID ids.NodeID) {
	b.streaklock.Lock()
	defer b.streaklock.Unlock()
	delete(b.failureStreaks, nodeID)
}

// RegisterResponse notes that a request to validator [validatorID] timed out
func (b *benchlist) RegisterFailure(nodeID ids.NodeID) {
	b.lock.Lock()
	defer b.lock.Unlock()

	if b.benchlistSet.Contains(nodeID) {
		// This validator is benched. Ignore failures until they're not.
		return
	}

	b.streaklock.Lock()
	failureStreak := b.failureStreaks[nodeID]
	// Increment consecutive failures
	failureStreak.consecutive++
	now := b.clock.Time()
	// Update first failure time
	if failureStreak.firstFailure.IsZero() {
		// This is the first consecutive failure
		failureStreak.firstFailure = now
	}
	b.failureStreaks[nodeID] = failureStreak
	b.streaklock.Unlock()

	if failureStreak.consecutive >= b.threshold && now.After(failureStreak.firstFailure.Add(b.minimumFailingDuration)) {
		b.bench(nodeID)
	}
}

// Assumes [b.lock] is held
// Assumes [nodeID] is not already benched
func (b *benchlist) bench(nodeID ids.NodeID) {
	validatorStake := b.vdrs.GetWeight(b.ctx.SubnetID, nodeID)
	if validatorStake == 0 {
		// We might want to bench a non-validator because they don't respond to
		// my Get requests, but we choose to only bench validators.
		return
	}

	benchedStake, err := b.vdrs.SubsetWeight(b.ctx.SubnetID, b.benchlistSet)
	if err != nil {
		b.ctx.Log.Error("error calculating benched stake",
			zap.Stringer("subnetID", b.ctx.SubnetID),
			zap.Error(err),
		)
		return
	}

	newBenchedStake, err := safemath.Add64(benchedStake, validatorStake)
	if err != nil {
		// This should never happen
		b.ctx.Log.Error("overflow calculating new benched stake",
			zap.Stringer("nodeID", nodeID),
		)
		return
	}

	totalStake, err := b.vdrs.TotalWeight(b.ctx.SubnetID)
	if err != nil {
		b.ctx.Log.Error("error calculating total stake",
			zap.Stringer("subnetID", b.ctx.SubnetID),
			zap.Error(err),
		)
		return
	}

	maxBenchedStake := float64(totalStake) * b.maxPortion

	if float64(newBenchedStake) > maxBenchedStake {
		b.ctx.Log.Debug("not benching node",
			zap.String("reason", "benched stake would exceed max"),
			zap.Stringer("nodeID", nodeID),
			zap.Float64("benchedStake", float64(newBenchedStake)),
			zap.Float64("maxBenchedStake", maxBenchedStake),
		)
		return
	}

	// Validator is benched for between [b.duration]/2 and [b.duration]
	now := b.clock.Time()
	minBenchDuration := b.duration / 2
	minBenchedUntil := now.Add(minBenchDuration)
	maxBenchedUntil := now.Add(b.duration)
	diff := maxBenchedUntil.Sub(minBenchedUntil)
	benchedUntil := minBenchedUntil.Add(time.Duration(rand.Float64() * float64(diff))) // #nosec G404

	// Add to benchlist times with randomized delay
	b.benchlistSet.Add(nodeID)
	b.benchable.Benched(b.ctx.ChainID, nodeID)

	b.streaklock.Lock()
	delete(b.failureStreaks, nodeID)
	b.streaklock.Unlock()

	b.benchedHeap.Push(nodeID, benchedUntil)
	b.ctx.Log.Debug("benching validator after consecutive failed queries",
		zap.Stringer("nodeID", nodeID),
		zap.Duration("benchDuration", benchedUntil.Sub(now)),
		zap.Int("numFailedQueries", b.threshold),
	)

	// Set [b.timer] to fire when next validator should leave bench
	b.setNextLeaveTime()

	// Update metrics
	b.metrics.numBenched.Set(float64(b.benchedHeap.Len()))
	b.metrics.weightBenched.Set(float64(newBenchedStake))
}
