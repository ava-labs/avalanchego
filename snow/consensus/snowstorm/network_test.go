// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/sampler"

	sbcon "github.com/ava-labs/avalanchego/snow/consensus/snowball"
)

type Network struct {
	params         sbcon.Parameters
	consumers      []*TestTx
	nodeTxs        []map[ids.ID]*TestTx
	nodes, running []Consensus
}

func (n *Network) shuffleConsumers() {
	s := sampler.NewUniform()
	_ = s.Initialize(uint64(len(n.consumers)))
	indices, _ := s.Sample(len(n.consumers))
	consumers := []*TestTx(nil)
	for _, index := range indices {
		consumers = append(consumers, n.consumers[int(index)])
	}
	n.consumers = consumers
}

func (n *Network) Initialize(
	params sbcon.Parameters,
	numColors,
	colorsPerConsumer,
	maxInputConflicts int,
) {
	n.params = params

	idCount := uint64(0)

	colorMap := map[ids.ID]int{}
	colors := []ids.ID{}
	for i := 0; i < numColors; i++ {
		idCount++
		color := ids.Empty.Prefix(idCount)
		colorMap[color] = i
		colors = append(colors, color)
	}

	count := map[ids.ID]int{}
	for len(colors) > 0 {
		selected := []ids.ID{}
		s := sampler.NewUniform()
		_ = s.Initialize(uint64(len(colors)))
		size := len(colors)
		if size > colorsPerConsumer {
			size = colorsPerConsumer
		}
		indices, _ := s.Sample(size)
		for _, index := range indices {
			selected = append(selected, colors[int(index)])
		}

		for _, sID := range selected {
			newCount := count[sID] + 1
			count[sID] = newCount
			if newCount >= maxInputConflicts {
				i := colorMap[sID]
				e := len(colorMap) - 1

				eID := colors[e]

				colorMap[eID] = i
				colors[i] = eID

				delete(colorMap, sID)
				colors = colors[:e]
			}
		}

		idCount++
		tx := &TestTx{TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(idCount),
			StatusV: choices.Processing,
		}}
		tx.InputIDsV = append(tx.InputIDsV, selected...)

		n.consumers = append(n.consumers, tx)
	}
}

func (n *Network) AddNode(cg Consensus) error {
	if err := cg.Initialize(snow.DefaultConsensusContextTest(), n.params); err != nil {
		return err
	}

	n.shuffleConsumers()

	txs := map[ids.ID]*TestTx{}
	for _, tx := range n.consumers {
		newTx := &TestTx{
			TestDecidable: choices.TestDecidable{
				IDV:     tx.ID(),
				StatusV: choices.Processing,
			},
			InputIDsV: tx.InputIDs(),
		}
		txs[newTx.ID()] = newTx

		if err := cg.Add(newTx); err != nil {
			return err
		}
	}

	n.nodeTxs = append(n.nodeTxs, txs)
	n.nodes = append(n.nodes, cg)
	n.running = append(n.running, cg)

	return nil
}

func (n *Network) Finalized() bool {
	return len(n.running) == 0
}

func (n *Network) Round() error {
	if len(n.running) == 0 {
		return nil
	}

	s := sampler.NewUniform()
	_ = s.Initialize(uint64(len(n.running)))
	runningInd, _ := s.Next()

	running := n.running[runningInd]

	_ = s.Initialize(uint64(len(n.nodes)))
	indices, _ := s.Sample(n.params.K)
	sampledColors := ids.Bag{}
	sampledColors.SetThreshold(n.params.Alpha)
	for _, index := range indices {
		peer := n.nodes[int(index)]
		peerTxs := n.nodeTxs[int(index)]

		preferences := peer.Preferences()
		for _, color := range preferences.List() {
			sampledColors.Add(color)
		}
		for _, tx := range peerTxs {
			if tx.Status() == choices.Accepted {
				sampledColors.Add(tx.ID())
			}
		}
	}

	if _, err := running.RecordPoll(sampledColors); err != nil {
		return err
	}

	// If this node has been finalized, remove it from the poller
	if running.Finalized() {
		newSize := len(n.running) - 1
		n.running[runningInd] = n.running[newSize]
		n.running = n.running[:newSize]
	}
	return nil
}

func (n *Network) Disagreement() bool {
	for _, color := range n.consumers {
		accepted := false
		rejected := false
		for _, nodeTx := range n.nodeTxs {
			tx := nodeTx[color.ID()]
			accepted = accepted || tx.Status() == choices.Accepted
			rejected = rejected || tx.Status() == choices.Rejected
		}
		if accepted && rejected {
			return true
		}
	}
	return false
}

func (n *Network) Agreement() bool {
	statuses := map[ids.ID]choices.Status{}
	for _, color := range n.consumers {
		for _, nodeTx := range n.nodeTxs {
			colorID := color.ID()
			tx := nodeTx[colorID]
			prevStatus, exists := statuses[colorID]
			if exists && prevStatus != tx.Status() {
				return false
			}
			statuses[colorID] = tx.Status()
		}
	}
	return !n.Disagreement()
}
