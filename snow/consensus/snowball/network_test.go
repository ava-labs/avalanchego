// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

type newConsensusFunc func(factory Factory, params Parameters, choice ids.ID) Consensus

type Network struct {
	params         Parameters
	colors         []ids.ID
	rngSource      sampler.Source
	nodes, running []Consensus
	factory        Factory
}

// Create a new network with [numColors] different possible colors to finalize.
func NewNetwork(factory Factory, params Parameters, numColors int, rngSource sampler.Source) *Network {
	n := &Network{
		params:    params,
		rngSource: rngSource,
		factory:   factory,
	}
	for i := 0; i < numColors; i++ {
		n.colors = append(n.colors, ids.Empty.Prefix(uint64(i)))
	}
	return n
}

func (n *Network) AddNode(newConsensusFunc newConsensusFunc) Consensus {
	s := sampler.NewDeterministicUniform(n.rngSource)
	s.Initialize(uint64(len(n.colors)))
	indices, _ := s.Sample(len(n.colors))

	consensus := newConsensusFunc(n.factory, n.params, n.colors[int(indices[0])])
	for _, index := range indices[1:] {
		consensus.Add(n.colors[int(index)])
	}

	n.nodes = append(n.nodes, consensus)
	if !consensus.Finalized() {
		n.running = append(n.running, consensus)
	}

	return consensus
}

// AddNodeSpecificColor adds a new consensus instance to the network which will
// initially prefer [initialPreference] and additionally adds each of the
// specified [options] to consensus.
func (n *Network) AddNodeSpecificColor(
	newConsensusFunc newConsensusFunc,
	initialPreference int,
	options []int,
) Consensus {
	consensus := newConsensusFunc(n.factory, n.params, n.colors[initialPreference])

	for _, i := range options {
		consensus.Add(n.colors[i])
	}

	n.nodes = append(n.nodes, consensus)
	if !consensus.Finalized() {
		n.running = append(n.running, consensus)
	}

	return consensus
}

// Finalized returns true iff every node added to the network has finished
// running.
func (n *Network) Finalized() bool {
	return len(n.running) == 0
}

// Round simulates a round of consensus by randomly selecting a running node and
// performing an unbiased poll of the nodes in the network for that node.
func (n *Network) Round() {
	if len(n.running) > 0 {
		s := sampler.NewDeterministicUniform(n.rngSource)

		s.Initialize(uint64(len(n.running)))
		runningInd, _ := s.Next()
		running := n.running[runningInd]

		s.Initialize(uint64(len(n.nodes)))
		count := min(n.params.K, len(n.nodes))
		indices, _ := s.Sample(count)
		sampledColors := bag.Bag[ids.ID]{}
		for _, index := range indices {
			peer := n.nodes[int(index)]
			sampledColors.Add(peer.Preference())
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

// Disagreement returns true iff there are any two nodes in the network that
// have finalized two different preferences.
func (n *Network) Disagreement() bool {
	// Iterate [i] to the index of the first node that has finalized.
	i := 0
	for ; i < len(n.nodes) && !n.nodes[i].Finalized(); i++ {
	}
	// If none of the nodes have finalized, then there is no disagreement.
	if i >= len(n.nodes) {
		return false
	}

	// Return true if any other finalized node has finalized a different
	// preference.
	pref := n.nodes[i].Preference()
	for ; i < len(n.nodes); i++ {
		if node := n.nodes[i]; node.Finalized() && pref != node.Preference() {
			return true
		}
	}
	return false
}

// Agreement returns true iff every node in the network prefers the same value.
func (n *Network) Agreement() bool {
	if len(n.nodes) == 0 {
		return true
	}
	pref := n.nodes[0].Preference()
	for _, node := range n.nodes {
		if pref != node.Preference() {
			return false
		}
	}
	return true
}
