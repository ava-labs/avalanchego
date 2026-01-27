// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"math/rand"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/math"
	"go.uber.org/zap"
)

const (
	halflife           = time.Minute
	unbenchProbability = .2
	benchProbability   = .5
)

// If is projected not to respond to queries, it will increase latencies on the
// network whenever that peer is polled.
//
// If we cannot terminate the poll early, then the poll will wait the full
// timeout before finalizing the poll and making progress. This can increase
// network latencies to an undesirable level.
//
// Therefore, nodes that are projected to fail are "benched" such that queries
// to that node fail immediately to avoid waiting up to the full network timeout
// for a response.
type benchlist struct {
	// Context of the chain this is the benchlist for
	ctx       *snow.ConsensusContext
	benchable Benchable

	lock  sync.RWMutex
	nodes map[ids.NodeID]*node

	jobs buffer.BlockingDeque[job]
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
) *benchlist {
	b := &benchlist{
		ctx:       ctx,
		benchable: benchable,
		nodes:     make(map[ids.NodeID]*node),
		jobs:      buffer.NewUnboundedBlockingDeque[job](1),
	}
	go b.run()
	return b
}

// RegisterResponse notes that we received a response from [nodeID] prior to the
// timeout firing.
func (b *benchlist) RegisterResponse(nodeID ids.NodeID) {
	b.lock.Lock()
	defer b.lock.Unlock()

	n := b.getNode(nodeID)
	n.failureProbability.Observe(0, time.Now())
	if !n.isBenched {
		return
	}

	if n.failureProbability.Read() < unbenchProbability {
		n.isBenched = false
		b.jobs.PushRight(job{
			nodeID: nodeID,
			bench:  false,
		})
	}
}

// RegisterFailure notes that a request to [nodeID] timed out
func (b *benchlist) RegisterFailure(nodeID ids.NodeID) {
	b.lock.Lock()
	defer b.lock.Unlock()

	n := b.getNode(nodeID)
	n.failureProbability.Observe(1, time.Now())
	if n.isBenched {
		return
	}

	if n.failureProbability.Read() > benchProbability {
		n.isBenched = true
		b.jobs.PushRight(job{
			nodeID: nodeID,
			bench:  true,
		})
	}
}

// IsBenched returns true if messages to [nodeID] should not be sent over the
// network and should immediately fail.
func (b *benchlist) IsBenched(nodeID ids.NodeID) bool {
	b.lock.RLock()
	defer b.lock.RUnlock()

	return b.getNode(nodeID).isBenched
}

func (b *benchlist) getNode(nodeID ids.NodeID) *node {
	if rand.Intn(10000) == 0 {
		p := make(map[ids.NodeID]float64, len(b.nodes))
		for id, n := range b.nodes {
			p[id] = n.failureProbability.Read()
		}
		b.ctx.Log.Info("benchlist node failure probabilities",
			zap.Stringer("chainID", b.ctx.ChainID),
			zap.Any("probabilities", p),
		)
	}

	if n, exists := b.nodes[nodeID]; exists {
		return n
	}

	n := &node{
		failureProbability: math.NewUninitializedAverager(halflife),
	}
	b.nodes[nodeID] = n
	return n
}

func (b *benchlist) run() {
	for {
		job, _ := b.jobs.PopLeft()
		if job.bench {
			b.ctx.Log.Info("adding node to benchlist",
				zap.Stringer("nodeID", job.nodeID),
			)
			b.benchable.Benched(b.ctx.ChainID, job.nodeID)
		} else {
			b.ctx.Log.Info("removing node from benchlist",
				zap.Stringer("nodeID", job.nodeID),
			)
			b.benchable.Unbenched(b.ctx.ChainID, job.nodeID)
		}
	}
}
