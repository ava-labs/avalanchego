// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer"
)

const (
	DefaultHalflife           = time.Minute
	DefaultUnbenchProbability = .2
	DefaultBenchProbability   = .5
	DefaultBenchDuration      = 5 * time.Minute

	success float64 = 0
	failure float64 = 1

	eventQueueInitSize = 16
)

// Config defines the configuration for a benchlist
type Config struct {
	Halflife           time.Duration `json:"halflife"`
	UnbenchProbability float64       `json:"unbenchProbability"`
	BenchProbability   float64       `json:"benchProbability"`
	BenchDuration      time.Duration `json:"benchDuration"`
	MaxPortion         float64       `json:"maxPortion"`
}

// event is a raw success/failure observation sent from any goroutine to the
// single consumer goroutine that owns all mutable node state. The observation
// time is captured at enqueue so the EWMA sees accurate timestamps even if the
// consumer goroutine is temporarily blocked.
type event struct {
	nodeID ids.NodeID
	value  float64   // success=0, failure=1
	time   time.Time // wall-clock time of the observation
}

// node tracks failure probability and bench state for a single node.
// Owned exclusively by the consumer goroutine — no external synchronization.
type node struct {
	nodeID             ids.NodeID
	failureProbability math.Averager
	currentFailureProb float64
	isBenched          bool
}

type benchOrderEntry struct {
	nodeID             ids.NodeID
	failureProbability float64
}

// If a remote node does not respond to a request, the local node waits for a
// timeout. This can cause elevated latencies if the local node frequently sends
// requests to the remote node.
//
// Therefore, we attempt to project whether or not a node is likely to respond
// to a query. If a node is projected to fail, it is "benched". While it is
// benched, queries to that node fail immediately to avoid waiting up to the
// full network timeout.
//
// If a node remains benched for longer than [benchDuration], it is
// automatically unbenched to give it another chance.
//
// All mutable state (nodes map, EWMA, timeout heap) is owned by a single
// consumer goroutine. Producers (RegisterResponse/RegisterFailure) enqueue
// events on an unbounded queue so they never block. IsBenched reads a
// published snapshot of the benched set.
type benchlist struct {
	ctx       *snow.ConsensusContext
	benchable Benchable

	vdrs          validators.Manager
	numBenched    prometheus.Gauge
	weightBenched prometheus.Gauge

	halflife           time.Duration
	unbenchProbability float64
	benchProbability   float64
	benchDuration      time.Duration
	maxPortion         float64

	// Event queue: producers push observations; the single consumer goroutine
	// pops and processes them. The queue is unbounded so producers never block.
	eventsMu   sync.Mutex
	events     buffer.Deque[event]
	eventReady chan struct{} // capacity 1

	// Owned by run goroutine only. All accesses are safe without additional
	// synchronization because external goroutines communicate observations via
	// [events], and these fields are only read/written while processing those
	// queued events (or timer fires) in [run].
	nodes            map[ids.NodeID]*node
	timeoutHeap      heap.Map[ids.NodeID, time.Time]
	benchedByFailure heap.Map[ids.NodeID, benchOrderEntry]

	// Protects only the benched set snapshot. Written by the consumer goroutine
	// after each state transition; read by IsBenched on any goroutine.
	lock    sync.RWMutex
	benched set.Set[ids.NodeID]

	shutdownOnce sync.Once
	shutdownChan chan struct{}
	shutdownDone chan struct{}
}

func newBenchlist(
	ctx *snow.ConsensusContext,
	benchable Benchable,
	validators validators.Manager,
	config Config,
	reg prometheus.Registerer,
) (*benchlist, error) {
	if config.MaxPortion < 0 || config.MaxPortion >= 1 {
		return nil, fmt.Errorf("max portion of benched stake must be in [0,1) but got %f", config.MaxPortion)
	}

	b := &benchlist{
		ctx:       ctx,
		benchable: benchable,

		vdrs: validators,
		numBenched: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "benched_num",
			Help: "Number of currently benched validators",
		}),
		weightBenched: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "benched_weight",
			Help: "Weight of currently benched validators",
		}),

		halflife:           config.Halflife,
		unbenchProbability: config.UnbenchProbability,
		benchProbability:   config.BenchProbability,
		benchDuration:      config.BenchDuration,
		maxPortion:         config.MaxPortion,
		events:             buffer.NewUnboundedDeque[event](eventQueueInitSize),
		eventReady:         make(chan struct{}, 1),
		nodes:              make(map[ids.NodeID]*node),
		timeoutHeap:        heap.NewMap[ids.NodeID, time.Time](time.Time.Before),
		benchedByFailure: heap.NewMap[ids.NodeID, benchOrderEntry](func(a, b benchOrderEntry) bool {
			if a.failureProbability != b.failureProbability {
				return a.failureProbability < b.failureProbability
			}
			return a.nodeID.Compare(b.nodeID) < 0
		}),
		shutdownChan: make(chan struct{}),
		shutdownDone: make(chan struct{}),
	}

	err := errors.Join(
		reg.Register(b.numBenched),
		reg.Register(b.weightBenched),
	)
	if err != nil {
		return nil, err
	}

	go b.run()
	return b, nil
}

// --- Public API (any goroutine) ---

// RegisterResponse notes that we received a response from nodeID prior to the
// timeout firing.
func (b *benchlist) RegisterResponse(nodeID ids.NodeID) {
	b.enqueue(event{nodeID: nodeID, value: success, time: time.Now()})
}

// RegisterFailure notes that a request to nodeID timed out.
func (b *benchlist) RegisterFailure(nodeID ids.NodeID) {
	b.enqueue(event{nodeID: nodeID, value: failure, time: time.Now()})
}

// enqueue adds the event to the unbounded queue and signals the consumer.
// Never blocks.
func (b *benchlist) enqueue(ev event) {
	b.eventsMu.Lock()
	b.events.PushRight(ev)
	b.eventsMu.Unlock()
	select {
	case b.eventReady <- struct{}{}:
	default:
	}
}

// IsBenched returns true if messages to nodeID should immediately fail.
func (b *benchlist) IsBenched(nodeID ids.NodeID) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.benched.Contains(nodeID)
}

// --- Consumer goroutine (single owner of all mutable node state) ---

// run is the consumer goroutine. It owns the nodes map, timeout heap, and is
// the only goroutine that calls Benched/Unbenched on the benchable.
func (b *benchlist) run() {
	defer close(b.shutdownDone)

	t := timer.StoppedTimer()
	defer t.Stop()

	for {
		select {
		case <-b.shutdownChan:
			return
		case <-b.eventReady:
		case <-t.C:
		}

		b.processEvents()
		b.processTimeouts()
		b.updateMetrics()
		b.resetTimer(t)
	}
}

func (b *benchlist) shutdown() {
	b.shutdownOnce.Do(func() {
		close(b.shutdownChan)
		<-b.shutdownDone
	})
}

// processEvents drains all queued observations and applies them.
func (b *benchlist) processEvents() {
	for {
		b.eventsMu.Lock()
		ev, ok := b.events.PopLeft()
		b.eventsMu.Unlock()
		if !ok {
			return
		}
		b.processObservation(ev)
	}
}

// processObservation updates a node's EWMA and transitions bench state if the
// failure probability crosses a threshold.
func (b *benchlist) processObservation(ev event) {
	b.processObservationWithMakeRoom(ev, b.tryMakeRoom)
}

func (b *benchlist) processObservationWithMakeRoom(
	ev event,
	tryMakeRoom func(ids.NodeID, float64) bool,
) {
	nodeID := ev.nodeID

	n, ok := b.nodes[nodeID]
	if b.vdrs.GetWeight(b.ctx.SubnetID, nodeID) == 0 {
		// Don't track non-validators unless they're currently benched. If they
		// aren't benched, prune any stale entry to avoid excess memory pressure.
		if ok && !n.isBenched {
			delete(b.nodes, nodeID)
		}
		return
	}
	if !ok {
		n = &node{
			nodeID:             nodeID,
			failureProbability: b.newFailureProbabilityAverager(ev.time),
			currentFailureProb: success,
		}
		b.nodes[nodeID] = n
	}

	n.failureProbability.Observe(ev.value, ev.time)
	p := n.failureProbability.Read()
	n.currentFailureProb = p
	if n.isBenched {
		b.benchedByFailure.Push(nodeID, benchOrderEntry{
			nodeID:             nodeID,
			failureProbability: p,
		})
	}

	switch {
	case !n.isBenched && p > b.benchProbability:
		if !tryMakeRoom(nodeID, p) {
			return
		}

		n.isBenched = true
		b.timeoutHeap.Push(nodeID, time.Now().Add(b.benchDuration))
		b.benchedByFailure.Push(nodeID, benchOrderEntry{
			nodeID:             nodeID,
			failureProbability: p,
		})

		b.lock.Lock()
		b.benched.Add(nodeID)
		b.lock.Unlock()

		b.ctx.Log.Debug("benching node",
			zap.Stringer("nodeID", nodeID),
			zap.Float64("failureProbability", p),
		)
		b.benchable.Benched(b.ctx.ChainID, nodeID)
	case n.isBenched && p < b.unbenchProbability:
		n.isBenched = false
		b.timeoutHeap.Remove(nodeID)
		b.benchedByFailure.Remove(nodeID)

		b.lock.Lock()
		b.benched.Remove(nodeID)
		b.lock.Unlock()

		b.ctx.Log.Debug("unbenching node",
			zap.Stringer("nodeID", nodeID),
			zap.Float64("failureProbability", p),
		)
		b.benchable.Unbenched(b.ctx.ChainID, nodeID)
	}
}

// processTimeouts unbenches any nodes whose bench duration has expired.
// Timeout-based unbench gives the node a clean EWMA slate so that a single
// failure after unbenching doesn't immediately re-bench it.
func (b *benchlist) processTimeouts() {
	now := time.Now()
	for {
		nodeID, deadline, ok := b.timeoutHeap.Peek()
		if !ok || deadline.After(now) {
			break
		}
		b.timeoutHeap.Pop()

		n, exists := b.nodes[nodeID]
		if !exists || !n.isBenched {
			continue
		}

		n.isBenched = false
		oldFailureProbability := n.failureProbability.Read()
		n.failureProbability = b.newFailureProbabilityAverager(now)
		n.currentFailureProb = n.failureProbability.Read()
		b.benchedByFailure.Remove(nodeID)

		b.lock.Lock()
		b.benched.Remove(nodeID)
		b.lock.Unlock()

		b.ctx.Log.Debug("unbenching node due to timeout",
			zap.Stringer("nodeID", nodeID),
			zap.Float64("oldFailureProbability", oldFailureProbability),
		)
		b.benchable.Unbenched(b.ctx.ChainID, nodeID)
	}
}

// newFailureProbabilityAverager creates a failure probability averager with an
// optimistic prior to slightly favor newly tracked nodes.
func (b *benchlist) newFailureProbabilityAverager(now time.Time) math.Averager {
	return math.NewAverager(success, b.halflife, now)
}

func (b *benchlist) benchingFits(nodeID ids.NodeID) (uint64, error) {
	incomingStake := b.vdrs.GetWeight(b.ctx.SubnetID, nodeID)
	if incomingStake == 0 {
		return 0, nil
	}

	benchedStake, err := b.benchedStake()
	if err != nil {
		return 0, err
	}

	totalStake, err := b.vdrs.TotalWeight(b.ctx.SubnetID)
	if err != nil {
		b.ctx.Log.Error("error calculating total stake",
			zap.Stringer("subnetID", b.ctx.SubnetID),
			zap.Error(err),
		)
		return 0, err
	}

	maxBenchedStake := float64(totalStake) * b.maxPortion

	// Fast path: benching fits directly without eviction.
	newBenchedStake, err := math.Add(benchedStake, incomingStake)
	if err != nil {
		b.ctx.Log.Error("overflow calculating new benched stake",
			zap.Stringer("nodeID", nodeID),
			zap.Uint64("benchedStake", benchedStake),
			zap.Uint64("incomingStake", incomingStake),
		)
		return 0, err
	}
	if float64(newBenchedStake) <= maxBenchedStake {
		return 0, nil
	}

	// If benching exceeds the max portion, we must evict >= targetEvictStake
	// so that benching the incoming node does not exceed the max portion.
	targetEvictStake := newBenchedStake - uint64(maxBenchedStake)
	return targetEvictStake, nil
}

// tryMakeRoom checks whether benching nodeID fits within maxPortion.
// If it fits directly, returns true. If not, it greedily evicts the currently
// benched nodes with the lowest failure probability that are still strictly
// better than the incoming node. Returns false if benching is not possible.
func (b *benchlist) tryMakeRoom(nodeID ids.NodeID, incomingFailureProbability float64) bool {
	targetEvictStake, err := b.benchingFits(nodeID)
	if err != nil {
		return false
	}
	if targetEvictStake == 0 {
		return true
	}

	var (
		evictedStake uint64
		evictEntries []benchOrderEntry
	)
	for evictedStake < targetEvictStake {
		entry, ok := b.peekEvictionCandidate()
		if !ok || entry.failureProbability >= incomingFailureProbability {
			break
		}

		b.benchedByFailure.Pop()
		evictEntries = append(evictEntries, entry)
		evictedStake += b.vdrs.GetWeight(b.ctx.SubnetID, entry.nodeID)
	}

	if evictedStake < targetEvictStake {
		for _, entry := range evictEntries {
			b.benchedByFailure.Push(entry.nodeID, entry)
		}
		b.logBenchRefused(nodeID, incomingFailureProbability, targetEvictStake, evictedStake)
		return false
	}

	for _, entry := range evictEntries {
		evictNode := b.nodes[entry.nodeID]
		evictNode.isBenched = false
		b.timeoutHeap.Remove(evictNode.nodeID)
		b.lock.Lock()
		b.benched.Remove(evictNode.nodeID)
		b.lock.Unlock()
		b.benchable.Unbenched(b.ctx.ChainID, evictNode.nodeID)
	}

	return true
}

func (b *benchlist) peekEvictionCandidate() (benchOrderEntry, bool) {
	for {
		_, entry, ok := b.benchedByFailure.Peek()
		if !ok {
			return benchOrderEntry{}, false
		}
		n, exists := b.nodes[entry.nodeID]
		if exists && n.isBenched && n.currentFailureProb == entry.failureProbability {
			return entry, true
		}

		b.benchedByFailure.Pop()
	}
}

func (b *benchlist) tryMakeRoomByScanSort(nodeID ids.NodeID, incomingFailureProbability float64) bool {
	targetEvictStake, err := b.benchingFits(nodeID)
	if err != nil {
		return false
	}
	if targetEvictStake == 0 {
		return true
	}

	// Scan the currently benched nodes and find all potential eviction candidates.
	var candidates []*node
	for _, node := range b.nodes {
		if !node.isBenched || node.currentFailureProb >= incomingFailureProbability {
			continue
		}

		candidates = append(candidates, node)
	}
	// Sort the candidates in ascending order of failure probability.
	// We want to select nodes in ascending order of failure probability, so that we
	// evict nodes from the benchlist with the lowest failure probability => maximize
	// probability of successful queries.
	slices.SortFunc(candidates, func(a, b *node) int {
		if a.currentFailureProb != b.currentFailureProb {
			return cmp.Compare(a.currentFailureProb, b.currentFailureProb)
		}
		return a.nodeID.Compare(b.nodeID)
	})

	// Select a sufficient set of candidates to evict to make room for the incoming node.
	var (
		evictedStake uint64
		evictNodes   []*node
	)
	for i, candidate := range candidates {
		candidateStake := b.vdrs.GetWeight(b.ctx.SubnetID, candidate.nodeID)
		evictedStake += candidateStake
		evictNodes = candidates[:i+1]
		if evictedStake >= targetEvictStake {
			break
		}
	}

	// If we couldn't evict enough stake to make room for the incoming node, skip
	// benching it and return early.
	if evictedStake < targetEvictStake {
		b.logBenchRefused(nodeID, incomingFailureProbability, targetEvictStake, evictedStake)
		return false
	}

	// Evict the selected candidates from the benchlist
	for _, evictNode := range evictNodes {
		evictNode.isBenched = false
		b.timeoutHeap.Remove(evictNode.nodeID)
		b.lock.Lock()
		b.benched.Remove(evictNode.nodeID)
		b.lock.Unlock()
		b.benchable.Unbenched(b.ctx.ChainID, evictNode.nodeID)
	}

	return true
}

func (b *benchlist) logBenchRefused(
	nodeID ids.NodeID,
	incomingFailureProbability float64,
	targetEvictStake uint64,
	evictedStake uint64,
) {
	benchedStake, err := b.benchedStake()
	if err != nil {
		return
	}
	totalStake, err := b.vdrs.TotalWeight(b.ctx.SubnetID)
	if err != nil {
		return
	}
	maxBenchedStake := float64(totalStake) * b.maxPortion
	incomingStake := b.vdrs.GetWeight(b.ctx.SubnetID, nodeID)
	newBenchedStake, err := math.Add(benchedStake, incomingStake)
	if err != nil {
		return
	}

	b.ctx.Log.Debug("not benching node",
		zap.String("reason", "benched stake would exceed max"),
		zap.Stringer("nodeID", nodeID),
		zap.Float64("incomingFailureProbability", incomingFailureProbability),
		zap.Float64("benchedStake", float64(newBenchedStake)),
		zap.Float64("maxBenchedStake", maxBenchedStake),
		zap.Float64("evictableStake", float64(evictedStake)),
		zap.Float64("targetEvictStake", float64(targetEvictStake)),
	)
}

// benchedStake returns the total stake weight of currently benched validators.
func (b *benchlist) benchedStake() (uint64, error) {
	var benchedNodeIDs set.Set[ids.NodeID]
	b.lock.RLock()
	benchedNodeIDs.Union(b.benched)
	b.lock.RUnlock()

	weight, err := b.vdrs.SubsetWeight(b.ctx.SubnetID, benchedNodeIDs)
	if err != nil {
		b.ctx.Log.Error("error calculating benched stake",
			zap.Stringer("subnetID", b.ctx.SubnetID),
			zap.Error(err),
		)
	}
	return weight, err
}

// resetTimer stops the timer and resets it to fire at the earliest deadline in
// the timeout heap. If the heap is empty, the timer remains stopped and will be
// reset when the next event arrives.
func (b *benchlist) resetTimer(timer *time.Timer) {
	if !timer.Stop() {
		// The default case is required because the run loop may have
		// already consumed the timer value via case <-timer.C.
		// If the timer has not delivered yet and we hit the default
		// path, it will trigger an extra iteration through the for loop
		// in run. This extra iteration does not cause an issue.
		select {
		case <-timer.C:
		default:
		}
	}

	if _, deadline, ok := b.timeoutHeap.Peek(); ok {
		timer.Reset(time.Until(deadline))
	}
}

func (b *benchlist) updateMetrics() {
	b.lock.RLock()
	numBenched := float64(b.benched.Len())
	b.lock.RUnlock()
	b.numBenched.Set(numBenched)

	weight, err := b.benchedStake()
	if err != nil {
		return
	}
	b.weightBenched.Set(float64(weight))
}
