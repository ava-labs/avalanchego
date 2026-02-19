// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"errors"
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
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

const (
	DefaultHalflife           = time.Minute
	DefaultUnbenchProbability = .2
	DefaultBenchProbability   = .5
	DefaultBenchDuration      = 5 * time.Minute

	success float64 = 0
	failure float64 = 1

	jobQueueInitSize = 16
)

// Config defines the configuration for a benchlist
type Config struct {
	Halflife           time.Duration `json:"halflife"`
	UnbenchProbability float64       `json:"unbenchProbability"`
	BenchProbability   float64       `json:"benchProbability"`
	BenchDuration      time.Duration `json:"benchDuration"`
}

// If a node does not respond to a request, the node waits for a timeout. This
// can cause elevated latencies if a peer is frequently sent requests.
//
// Therefore, we attempt to project whether or not a peer is likely to respond
// to a query. If a node is projected to fail, it is "benched". While it is
// benched, queries to that node fail immediately to avoid waiting up to the
// full network timeout.
//
// If a node remains benched for longer than [benchDuration], it is
// automatically unbenched to give it another chance.
type benchlist struct {
	ctx       *snow.ConsensusContext
	benchable Benchable

	vdrs          validators.Manager
	numBenched    prometheus.Gauge
	weightBenched prometheus.Gauge

	clock              mockable.Clock
	halflife           time.Duration
	unbenchProbability float64
	benchProbability   float64
	benchDuration      time.Duration

	// Notifications to the benchable are processed in a separate goroutine to
	// avoid any potential reentrant locking between the benchlist and the
	// benchable.
	jobs     *buffer.UnboundedBlockingDeque[job]
	jobReady chan struct{}

	lock  sync.RWMutex
	nodes map[ids.NodeID]*node
}

type node struct {
	failureProbability math.Averager
	isBenched          bool
	benchedAt          time.Time
}

type job struct {
	nodeID ids.NodeID
	bench  bool
}

func newBenchlist(
	ctx *snow.ConsensusContext,
	benchable Benchable,
	validators validators.Manager,
	config Config,
	reg prometheus.Registerer,
) (*benchlist, error) {
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
		jobs:               buffer.NewUnboundedBlockingDeque[job](jobQueueInitSize),
		jobReady:           make(chan struct{}, 1),
		nodes:              make(map[ids.NodeID]*node),
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

// RegisterResponse notes that we received a response from nodeID prior to the
// timeout firing.
func (b *benchlist) RegisterResponse(nodeID ids.NodeID) {
	b.observe(nodeID, success)
}

// RegisterFailure notes that a request to nodeID timed out.
func (b *benchlist) RegisterFailure(nodeID ids.NodeID) {
	b.observe(nodeID, failure)
}

func (b *benchlist) observe(nodeID ids.NodeID, v float64) {
	b.lock.Lock()
	defer b.lock.Unlock()

	n, ok := b.nodes[nodeID]
	if weight := b.vdrs.GetWeight(b.ctx.SubnetID, nodeID); weight == 0 && (!ok || !n.isBenched) {
		// Don't track non-validators unless they're already benched to avoid
		// excess memory pressure.
		return
	}
	if !ok {
		n = &node{
			failureProbability: math.NewUninitializedAverager(b.halflife),
		}
		b.nodes[nodeID] = n
	}

	n.failureProbability.Observe(v, b.clock.Time())

	p := n.failureProbability.Read()
	shouldBench := !n.isBenched && p > b.benchProbability
	shouldUnbench := n.isBenched && p < b.unbenchProbability
	if shouldBench || shouldUnbench {
		n.isBenched = !n.isBenched
		if n.isBenched {
			n.benchedAt = b.clock.Time()
		}
		_ = b.jobs.PushRight(job{
			nodeID: nodeID,
			bench:  n.isBenched,
		})
		select {
		case b.jobReady <- struct{}{}:
		default:
		}
	}
}

// IsBenched returns true if messages to nodeID should immediately fail.
func (b *benchlist) IsBenched(nodeID ids.NodeID) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()

	n, ok := b.nodes[nodeID]
	return ok && n.isBenched
}

// TODO: Close goroutine within run during shutdown.
func (b *benchlist) run() {
	var (
		benched     set.Set[ids.NodeID]
		timeoutHeap = heap.NewMap[ids.NodeID, time.Time](time.Time.Before)
		timer       = time.NewTimer(0)
	)
	defer timer.Stop()

	// Drain the initial timer fire since nothing is benched yet.
	<-timer.C

	for {
		select {
		case <-b.jobReady:
			for b.jobs.Len() > 0 {
				j, _ := b.jobs.PopLeft()
				b.ctx.Log.Debug("updating benchlist",
					zap.Stringer("nodeID", j.nodeID),
					zap.Bool("benching", j.bench),
				)

				if j.bench {
					// The producer path (observe) is the source of truth for
					// b.nodes transitions triggered by EWMA. By the time a bench
					// job is emitted, n.isBenched was already set under b.lock.
					// This consumer branch only drives callbacks/metrics and tracks
					// callback-visible deadlines.
					// Always update the timeout deadline. If the node is
					// already benched from the consumer's perspective, this
					// extends the deadline without a duplicate Benched
					// notification.
					timeoutHeap.Push(j.nodeID, time.Now().Add(b.benchDuration))
					if !benched.Contains(j.nodeID) {
						benched.Add(j.nodeID)
						b.benchable.Benched(b.ctx.ChainID, j.nodeID)
					}
				} else if benched.Contains(j.nodeID) {
					// Same as above: EWMA unbench jobs don't update b.nodes here
					// because observe already flipped n.isBenched before enqueuing.
					// Guard: only unbench if the consumer still considers
					// the node benched, avoiding duplicate Unbenched calls
					// when both EWMA and timeout race to unbench.
					benched.Remove(j.nodeID)
					timeoutHeap.Remove(j.nodeID)
					b.benchable.Unbenched(b.ctx.ChainID, j.nodeID)
				}
			}

		case <-timer.C:
			now := time.Now()
			for {
				nodeID, deadline, ok := timeoutHeap.Peek()
				if !ok || deadline.After(now) {
					break
				}
				timeoutHeap.Pop()
				benched.Remove(nodeID)
				b.lock.Lock()
				// Timeout unbenching originates in this consumer goroutine
				// (not in observe), so we must synchronize b.nodes here to
				// keep IsBenched consistent with callback-visible state.
				// Timeout-based unbench gives the node a clean slate.
				if n, exists := b.nodes[nodeID]; exists {
					n.isBenched = false
					n.failureProbability = math.NewUninitializedAverager(b.halflife)
				}
				b.lock.Unlock()

				b.ctx.Log.Debug("unbenching node due to timeout",
					zap.Stringer("nodeID", nodeID),
				)
				b.benchable.Unbenched(b.ctx.ChainID, nodeID)
			}
		}

		b.updateMetrics(benched)
		b.resetTimer(timer, &timeoutHeap)
	}
}

// resetTimer stops the timer and resets it to fire at the earliest deadline in
// the timeout heap. If the heap is empty, the timer remains stopped and will be
// reset when the next bench job arrives.
func (*benchlist) resetTimer(timer *time.Timer, timeoutHeap *heap.Map[ids.NodeID, time.Time]) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	if _, deadline, ok := timeoutHeap.Peek(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			remaining = 0
		}
		timer.Reset(remaining)
	}
}

func (b *benchlist) updateMetrics(benched set.Set[ids.NodeID]) {
	b.numBenched.Set(float64(benched.Len()))
	benchedStake, err := b.vdrs.SubsetWeight(b.ctx.SubnetID, benched)
	if err != nil {
		b.ctx.Log.Error("error calculating benched stake",
			zap.Stringer("subnetID", b.ctx.SubnetID),
			zap.Error(err),
		)
		return
	}
	b.weightBenched.Set(float64(benchedStake))
}
