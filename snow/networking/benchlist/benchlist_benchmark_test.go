// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/heap"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
)

type noopBenchable struct{}

func (noopBenchable) Benched(ids.ID, ids.NodeID)   {}
func (noopBenchable) Unbenched(ids.ID, ids.NodeID) {}

type fixedAverager struct {
	value float64
}

func (a *fixedAverager) Observe(float64, time.Time) {}

func (a *fixedAverager) Read() float64 {
	return a.value
}

type benchmarkScenario struct {
	name  string
	setup func(*testing.B, observationStrategy) benchmarkRunner
}

type benchmarkRunner interface {
	run(*testing.B)
}

type observationStrategy func(*benchlist, event)

type happyPathRunner struct {
	benchlist *benchlist
	nodeIDs   []ids.NodeID
	now       time.Time
	observe   observationStrategy
}

func (r *happyPathRunner) run(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r.now = r.now.Add(time.Nanosecond)
		nodeID := r.nodeIDs[i%len(r.nodeIDs)]
		r.observe(r.benchlist, event{
			nodeID: nodeID,
			value:  failure,
			time:   r.now,
		})
	}
}

type evictionPathRunner struct {
	benchlist   *benchlist
	incomingIDs []ids.NodeID
	now         time.Time
	observe     observationStrategy
}

func (r *evictionPathRunner) run(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r.now = r.now.Add(time.Nanosecond)
		nodeID := r.incomingIDs[i]
		probability := 0.75 + (0.24 * float64(i+1) / float64(b.N+1))
		n := r.benchlist.nodes[nodeID]
		n.currentFailureProb = probability
		n.failureProbability = &fixedAverager{value: probability}

		r.observe(r.benchlist, event{
			nodeID: nodeID,
			value:  failure,
			time:   r.now,
		})
	}
}

func BenchmarkHandleObservation(b *testing.B) {
	strategies := []struct {
		name    string
		observe observationStrategy
	}{
		{
			name:    "scan_sort",
			observe: processObservationByScanSort,
		},
		{
			name:    "ordered_heap",
			observe: (*benchlist).processObservation,
		},
	}
	scenarios := []benchmarkScenario{
		{
			name:  "happy_path",
			setup: newHappyPathRunner,
		},
		{
			name:  "eviction_path",
			setup: newEvictionPathRunner,
		},
	}

	for _, scenario := range scenarios {
		for _, strategy := range strategies {
			b.Run(fmt.Sprintf("%s/%s", scenario.name, strategy.name), func(b *testing.B) {
				runner := scenario.setup(b, strategy.observe)
				b.ResetTimer()
				runner.run(b)
			})
		}
	}
}

func newHappyPathRunner(b *testing.B, observe observationStrategy) benchmarkRunner {
	const (
		nodeCount    = 8_192
		benchedCount = 4_096
	)

	benchlist, nodeIDs := newBenchmarkBenchlist(b, nodeCount, Config{
		Halflife:           DefaultHalflife,
		UnbenchProbability: DefaultUnbenchProbability,
		BenchProbability:   DefaultBenchProbability,
		BenchDuration:      DefaultBenchDuration,
		MaxPortion:         0.95,
	})
	now := time.Unix(0, 0)
	for i := 0; i < benchedCount; i++ {
		nodeID := nodeIDs[i]
		probability := 0.90
		benchlist.nodes[nodeID] = &node{
			nodeID:             nodeID,
			failureProbability: &fixedAverager{value: probability},
			currentFailureProb: probability,
			isBenched:          true,
		}
		benchlist.timeoutHeap.Push(nodeID, now.Add(DefaultBenchDuration))
		benchlist.benchedByFailure.Push(nodeID, benchOrderEntry{
			nodeID:             nodeID,
			failureProbability: probability,
		})
		benchlist.benched.Add(nodeID)
	}

	return &happyPathRunner{
		benchlist: benchlist,
		nodeIDs:   nodeIDs[:benchedCount],
		now:       now,
		observe:   observe,
	}
}

func newEvictionPathRunner(b *testing.B, observe observationStrategy) benchmarkRunner {
	const benchCapacity = 2_048

	nodeCount := benchCapacity + b.N + 1
	benchlist, nodeIDs := newBenchmarkBenchlist(b, nodeCount, Config{
		Halflife:           DefaultHalflife,
		UnbenchProbability: DefaultUnbenchProbability,
		BenchProbability:   DefaultBenchProbability,
		BenchDuration:      DefaultBenchDuration,
		MaxPortion:         float64(benchCapacity) / float64(nodeCount),
	})
	now := time.Unix(0, 0)
	for i := 0; i < benchCapacity; i++ {
		nodeID := nodeIDs[i]
		probability := 0.51 + (0.2 * float64(i) / float64(benchCapacity))
		benchlist.nodes[nodeID] = &node{
			nodeID:             nodeID,
			failureProbability: &fixedAverager{value: probability},
			currentFailureProb: probability,
			isBenched:          true,
		}
		benchlist.timeoutHeap.Push(nodeID, now.Add(DefaultBenchDuration))
		benchlist.benchedByFailure.Push(nodeID, benchOrderEntry{
			nodeID:             nodeID,
			failureProbability: probability,
		})
		benchlist.benched.Add(nodeID)
	}
	for _, nodeID := range nodeIDs[benchCapacity:] {
		benchlist.nodes[nodeID] = &node{
			nodeID:             nodeID,
			failureProbability: &fixedAverager{value: success},
			currentFailureProb: success,
		}
	}

	return &evictionPathRunner{
		benchlist:   benchlist,
		incomingIDs: nodeIDs[benchCapacity:],
		now:         now,
		observe:     observe,
	}
}

func processObservationByScanSort(b *benchlist, ev event) {
	nodeID := ev.nodeID

	n, ok := b.nodes[nodeID]
	if b.vdrs.GetWeight(b.ctx.SubnetID, nodeID) == 0 {
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

	switch {
	case !n.isBenched && p > b.benchProbability:
		if !b.tryMakeRoomByScanSort(nodeID, p) {
			return
		}

		n.isBenched = true
		b.timeoutHeap.Push(nodeID, time.Now().Add(b.benchDuration))

		b.lock.Lock()
		b.benched.Add(nodeID)
		b.lock.Unlock()

		b.benchable.Benched(b.ctx.ChainID, nodeID)
	case n.isBenched && p < b.unbenchProbability:
		n.isBenched = false
		b.timeoutHeap.Remove(nodeID)

		b.lock.Lock()
		b.benched.Remove(nodeID)
		b.lock.Unlock()

		b.benchable.Unbenched(b.ctx.ChainID, nodeID)
	}
}

func newBenchmarkBenchlist(b *testing.B, nodeCount int, config Config) (*benchlist, []ids.NodeID) {
	snowCtx := snowtest.Context(b, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	vdrs := validators.NewManager()
	nodeIDs := make([]ids.NodeID, nodeCount)
	for i := range nodeCount {
		nodeID := ids.GenerateTestNodeID()
		nodeIDs[i] = nodeID
		if err := vdrs.AddStaker(ctx.SubnetID, nodeID, nil, ids.Empty, 1); err != nil {
			b.Fatal(err)
		}
	}

	return &benchlist{
		ctx:                ctx,
		benchable:          noopBenchable{},
		vdrs:               vdrs,
		halflife:           config.Halflife,
		unbenchProbability: config.UnbenchProbability,
		benchProbability:   config.BenchProbability,
		benchDuration:      config.BenchDuration,
		maxPortion:         config.MaxPortion,
		nodes:              make(map[ids.NodeID]*node, nodeCount),
		timeoutHeap:        heap.NewMap[ids.NodeID, time.Time](time.Time.Before),
		benchedByFailure: heap.NewMap[ids.NodeID, benchOrderEntry](func(a, b benchOrderEntry) bool {
			if a.failureProbability != b.failureProbability {
				return a.failureProbability < b.failureProbability
			}
			return a.nodeID.Compare(b.nodeID) < 0
		}),
		benched: set.Set[ids.NodeID]{},
	}, nodeIDs
}

var _ math.Averager = (*fixedAverager)(nil)
