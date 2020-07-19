// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/utils/random"
)

type Network struct {
	params         snowball.Parameters
	consumers      []*TestTx
	nodeTxs        []map[[32]byte]*TestTx
	nodes, running []Consensus
}

func (n *Network) shuffleConsumers() {
	s := random.Uniform{N: len(n.consumers)}
	consumers := []*TestTx(nil)
	for s.CanSample() {
		consumers = append(consumers, n.consumers[s.Sample()])
	}
	n.consumers = consumers
}

func (n *Network) Initialize(params snowball.Parameters, numColors, colorsPerConsumer, maxInputConflicts int) {
	n.params = params

	idCount := uint64(0)

	colorMap := map[[32]byte]int{}
	colors := []ids.ID{}
	for i := 0; i < numColors; i++ {
		idCount++
		color := ids.Empty.Prefix(idCount)
		colorMap[color.Key()] = i
		colors = append(colors, color)
	}

	count := map[[32]byte]int{}
	for len(colors) > 0 {
		selected := []ids.ID{}
		sampler := random.Uniform{N: len(colors)}
		for i := 0; i < colorsPerConsumer && sampler.CanSample(); i++ {
			selected = append(selected, colors[sampler.Sample()])
		}

		for _, sID := range selected {
			sKey := sID.Key()
			newCount := count[sKey] + 1
			count[sKey] = newCount
			if newCount >= maxInputConflicts {
				i := colorMap[sKey]
				e := len(colorMap) - 1

				eID := colors[e]
				eKey := eID.Key()

				colorMap[eKey] = i
				colors[i] = eID

				delete(colorMap, sKey)
				colors = colors[:e]
			}
		}

		idCount++
		tx := &TestTx{TestDecidable: choices.TestDecidable{
			IDV:     ids.Empty.Prefix(idCount),
			StatusV: choices.Processing,
		}}
		tx.InputIDsV.Add(selected...)

		n.consumers = append(n.consumers, tx)
	}
}

func (n *Network) AddNode(cg Consensus) {
	cg.Initialize(snow.DefaultContextTest(), n.params)

	n.shuffleConsumers()

	txs := map[[32]byte]*TestTx{}
	for _, tx := range n.consumers {
		newTx := &TestTx{
			TestDecidable: choices.TestDecidable{
				IDV:     tx.ID(),
				StatusV: choices.Processing,
			},
			InputIDsV: tx.InputIDs(),
		}
		txs[newTx.ID().Key()] = newTx

		cg.Add(newTx)
	}

	n.nodeTxs = append(n.nodeTxs, txs)
	n.nodes = append(n.nodes, cg)
	n.running = append(n.running, cg)
}

func (n *Network) Finalized() bool {
	return len(n.running) == 0
}

func (n *Network) Round() {
	if len(n.running) > 0 {
		runningInd := random.Rand(0, len(n.running))
		running := n.running[runningInd]

		sampler := random.Uniform{N: len(n.nodes)}
		sampledColors := ids.Bag{}
		sampledColors.SetThreshold(n.params.Alpha)
		for i := 0; i < n.params.K; i++ {
			sample := sampler.Sample()
			peer := n.nodes[sample]
			peerTxs := n.nodeTxs[sample]

			if peer != running {
				preferences := peer.Preferences()
				for _, color := range preferences.List() {
					sampledColors.Add(color)
				}
				for _, tx := range peerTxs {
					if tx.Status() == choices.Accepted {
						sampledColors.Add(tx.ID())
					}
				}
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

func (n *Network) Disagreement() bool {
	for _, color := range n.consumers {
		accepted := false
		rejected := false
		for _, nodeTx := range n.nodeTxs {
			tx := nodeTx[color.ID().Key()]
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
	statuses := map[[32]byte]choices.Status{}
	for _, color := range n.consumers {
		for _, nodeTx := range n.nodeTxs {
			key := color.ID().Key()
			tx := nodeTx[key]
			prevStatus, exists := statuses[key]
			if exists && prevStatus != tx.Status() {
				return false
			}
			statuses[key] = tx.Status()
		}
	}
	return !n.Disagreement()
}
