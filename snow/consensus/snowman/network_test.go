// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bag"
	"github.com/ava-labs/avalanchego/utils/sampler"
)

type Network struct {
	params         snowball.Parameters
	colors         []*TestBlock
	rngSource      sampler.Source
	nodes, running []Consensus
}

func NewNetwork(params snowball.Parameters, numColors int, rngSource sampler.Source) *Network {
	n := &Network{
		params: params,
		colors: []*TestBlock{{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(rngSource.Uint64()),
				StatusV: choices.Processing,
			},
			ParentV: Genesis.IDV,
			HeightV: 1,
		}},
		rngSource: rngSource,
	}

	s := sampler.NewDeterministicUniform(n.rngSource)
	for i := 1; i < numColors; i++ {
		s.Initialize(uint64(len(n.colors)))
		dependencyInd, _ := s.Next()
		dependency := n.colors[dependencyInd]
		n.colors = append(n.colors, &TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     ids.Empty.Prefix(rngSource.Uint64()),
				StatusV: choices.Processing,
			},
			ParentV: dependency.IDV,
			HeightV: dependency.HeightV + 1,
		})
	}
	return n
}

func (n *Network) shuffleColors() {
	s := sampler.NewDeterministicUniform(n.rngSource)
	s.Initialize(uint64(len(n.colors)))
	indices, _ := s.Sample(len(n.colors))
	colors := []*TestBlock(nil)
	for _, index := range indices {
		colors = append(colors, n.colors[int(index)])
	}
	n.colors = colors
	utils.Sort(n.colors)
}

func (n *Network) AddNode(sm Consensus) error {
	if err := sm.Initialize(snow.DefaultConsensusContextTest(), n.params, Genesis.ID(), Genesis.Height(), Genesis.Timestamp()); err != nil {
		return err
	}

	n.shuffleColors()
	deps := map[ids.ID]ids.ID{}
	for _, blk := range n.colors {
		myDep, found := deps[blk.ParentV]
		if !found {
			myDep = blk.Parent()
		}
		myVtx := &TestBlock{
			TestDecidable: choices.TestDecidable{
				IDV:     blk.ID(),
				StatusV: blk.Status(),
			},
			ParentV: myDep,
			HeightV: blk.Height(),
			VerifyV: blk.Verify(context.Background()),
			BytesV:  blk.Bytes(),
		}
		if err := sm.Add(context.Background(), myVtx); err != nil {
			return err
		}
		deps[myVtx.ID()] = myDep
	}
	n.nodes = append(n.nodes, sm)
	n.running = append(n.running, sm)
	return nil
}

func (n *Network) Finalized() bool {
	return len(n.running) == 0
}

func (n *Network) Round() error {
	if len(n.running) == 0 {
		return nil
	}

	s := sampler.NewDeterministicUniform(n.rngSource)
	s.Initialize(uint64(len(n.running)))

	runningInd, _ := s.Next()
	running := n.running[runningInd]

	s.Initialize(uint64(len(n.nodes)))
	indices, _ := s.Sample(n.params.K)
	sampledColors := bag.Bag[ids.ID]{}
	for _, index := range indices {
		peer := n.nodes[int(index)]
		sampledColors.Add(peer.Preference())
	}

	if err := running.RecordPoll(context.Background(), sampledColors); err != nil {
		return err
	}

	// If this node has been finalized, remove it from the poller
	if running.NumProcessing() == 0 {
		newSize := len(n.running) - 1
		n.running[runningInd] = n.running[newSize]
		n.running = n.running[:newSize]
	}

	return nil
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
