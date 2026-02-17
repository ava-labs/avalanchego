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
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

const (
	halflife           = time.Minute
	unbenchProbability = .2
	benchProbability   = .5

	success float64 = 0
	failure float64 = 1
)

// If a node does not respond to a request, the node waits for a timeout. This
// can cause elevated latencies if a peer is frequently sent requests.
//
// Therefore, we attempt to project whether or not a peer is likely to respond
// to a query. If a node is projected to fail, it is "benched". While it is
// benched, queries to that node fail immediately to avoid waiting up to the
// full network timeout.
type benchlist struct {
	ctx       *snow.ConsensusContext
	benchable Benchable

	vdrs          validators.Manager
	numBenched    prometheus.Gauge
	weightBenched prometheus.Gauge

	clock mockable.Clock

	// Notifications to the benchable are processed in a separate goroutine to
	// avoid any potential reentrant locking.
	jobs buffer.BlockingDeque[job]

	lock  sync.RWMutex
	nodes map[ids.NodeID]*node
}

type node struct {
	failureProbability math.Averager
	isBenched          bool
}

type job struct {
	nodeID ids.NodeID
	bench  bool
}

func newBenchlist(
	ctx *snow.ConsensusContext,
	benchable Benchable,
	validators validators.Manager,
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

		jobs:  buffer.NewUnboundedBlockingDeque[job](1),
		nodes: make(map[ids.NodeID]*node),
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
			failureProbability: math.NewUninitializedAverager(halflife),
		}
		b.nodes[nodeID] = n
	}

	n.failureProbability.Observe(v, b.clock.Time())

	p := n.failureProbability.Read()
	shouldBench := !n.isBenched && p > benchProbability
	shouldUnbench := n.isBenched && p < unbenchProbability
	if shouldBench || shouldUnbench {
		n.isBenched = !n.isBenched
		b.jobs.PushRight(job{
			nodeID: nodeID,
			bench:  n.isBenched,
		})
	}
}

// IsBenched returns true if messages to nodeID should immediately fail.
func (b *benchlist) IsBenched(nodeID ids.NodeID) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()

	n, ok := b.nodes[nodeID]
	return ok && n.isBenched
}

// TODO: Close this goroutine during node shutdown
func (b *benchlist) run() {
	var benched set.Set[ids.NodeID]
	for {
		job, ok := b.jobs.PopLeft()
		if !ok {
			return
		}

		b.ctx.Log.Debug("updating benchlist",
			zap.Stringer("nodeID", job.nodeID),
			zap.Bool("benching", job.bench),
		)
		if job.bench {
			benched.Add(job.nodeID)
		} else {
			benched.Remove(job.nodeID)
		}

		b.numBenched.Set(float64(benched.Len()))
		benchedStake, err := b.vdrs.SubsetWeight(b.ctx.SubnetID, benched)
		if err != nil {
			b.ctx.Log.Error("error calculating benched stake",
				zap.Stringer("subnetID", b.ctx.SubnetID),
				zap.Error(err),
			)
			continue
		}
		b.weightBenched.Set(float64(benchedStake))

		// Notify the benchable of the change last so that all internal state is
		// updated before the notification is sent.
		if job.bench {
			b.benchable.Benched(b.ctx.ChainID, job.nodeID)
		} else {
			b.benchable.Unbenched(b.ctx.ChainID, job.nodeID)
		}
	}
}
