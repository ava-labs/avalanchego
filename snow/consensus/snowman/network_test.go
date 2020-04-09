// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"math"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/utils/random"
)

type Network struct {
	params         snowball.Parameters
	colors         []*TestBlock
	nodes, running []Consensus
}

func (n *Network) shuffleColors() {
	s := random.Uniform{N: len(n.colors)}
	colors := []*TestBlock(nil)
	for s.CanSample() {
		colors = append(colors, n.colors[s.Sample()])
	}
	n.colors = colors
	SortVts(n.colors)
}

func (n *Network) Initialize(params snowball.Parameters, numColors int) {
	n.params = params
	n.colors = append(n.colors, &TestBlock{
		parent: Genesis,
		id:     ids.Empty.Prefix(uint64(random.Rand(0, math.MaxInt64))),
		status: choices.Processing,
	})

	for i := 1; i < numColors; i++ {
		dependency := n.colors[random.Rand(0, len(n.colors))]
		n.colors = append(n.colors, &TestBlock{
			parent: dependency,
			id:     ids.Empty.Prefix(uint64(random.Rand(0, math.MaxInt64))),
			height: dependency.height + 1,
			status: choices.Processing,
		})
	}
}

func (n *Network) AddNode(sm Consensus) {
	sm.Initialize(snow.DefaultContextTest(), n.params, Genesis.ID())

	n.shuffleColors()
	deps := map[[32]byte]Block{}
	for _, blk := range n.colors {
		myDep, found := deps[blk.parent.ID().Key()]
		if !found {
			myDep = blk.parent
		}
		myVtx := &TestBlock{
			parent: myDep,
			id:     blk.id,
			height: blk.height,
			status: blk.status,
		}
		sm.Add(myVtx)
		deps[myVtx.ID().Key()] = myDep
	}
	n.nodes = append(n.nodes, sm)
	n.running = append(n.running, sm)
}

func (n *Network) Finalized() bool { return len(n.running) == 0 }

func (n *Network) Round() {
	if len(n.running) > 0 {
		runningInd := random.Rand(0, len(n.running))
		running := n.running[runningInd]

		sampler := random.Uniform{N: len(n.nodes)}
		sampledColors := ids.Bag{}
		for i := 0; i < n.params.K; i++ {
			peer := n.nodes[sampler.Sample()]
			if peer != running {
				sampledColors.Add(peer.Preference())
			} else {
				i-- // So that we still sample k people
			}
		}

		running.RecordPoll(sampledColors)

		// If this node has been finalized, remove it from the poller
		if running.Finalized() {
			newSize := len(n.running) - 1
			n.running[runningInd] = n.running[newSize]
			n.running = n.running[:newSize]
		}
	}
}

func (n *Network) Agreement() bool {
	if len(n.nodes) == 0 {
		return true
	}
	pref := n.nodes[0].Preference()
	for _, node := range n.nodes {
		if !pref.Equals(node.Preference()) {
			return false
		}
	}
	return true
}
