// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowball

import (
	"math/rand"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

type Network struct {
	params         Parameters
	colors         []ids.ID
	nodes, running []Consensus
}

func (n *Network) Initialize(params Parameters, numColors int) {
	n.params = params
	for i := 0; i < numColors; i++ {
		n.colors = append(n.colors, ids.Empty.Prefix(uint64(i)))
	}
}

func (n *Network) AddNode(sb Consensus) {
	s := sampler.NewUniform()
	_ = s.Initialize(uint64(len(n.colors)))
	indices, _ := s.Sample(len(n.colors))
	sb.Initialize(n.params, n.colors[int(indices[0])])
	for _, index := range indices[1:] {
		sb.Add(n.colors[int(index)])
	}

	n.nodes = append(n.nodes, sb)
	if !sb.Finalized() {
		n.running = append(n.running, sb)
	}
}

func (n *Network) AddNodeSpecificColor(sb Consensus, indices []int) {
	sb.Initialize(n.params, n.colors[indices[0]])
	for _, i := range indices[1:] {
		sb.Add(n.colors[i])
	}

	n.nodes = append(n.nodes, sb)
	if !sb.Finalized() {
		n.running = append(n.running, sb)
	}
}

func (n *Network) Finalized() bool {
	return len(n.running) == 0
}

func (n *Network) Round() {
	if len(n.running) > 0 {
		runningInd := rand.Intn(len(n.running)) // #nosec G404
		running := n.running[runningInd]

		s := sampler.NewUniform()
		_ = s.Initialize(uint64(len(n.nodes)))
		count := len(n.nodes)
		if count > n.params.K {
			count = n.params.K
		}
		indices, _ := s.Sample(count)
		sampledColors := ids.Bag{}
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

func (n *Network) Disagreement() bool {
	i := 0
	for ; i < len(n.nodes) && !n.nodes[i].Finalized(); i++ {
	}
	if i < len(n.nodes) {
		pref := n.nodes[i].Preference()
		for ; i < len(n.nodes); i++ {
			if node := n.nodes[i]; node.Finalized() && pref != node.Preference() {
				return true
			}
		}
	}
	return false
}

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
