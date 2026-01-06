// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"

	"go.uber.org/zap"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Poll = (*Majority)(nil)

// Majority implements the bootstrapping poll to filter the initial set of
// potentially acceptable blocks into a set of accepted blocks to sync to.
//
// Once the last accepted blocks have been fetched from the initial set of
// peers, the set of blocks are sent to all peers. Each peer is expected to
// filter the provided blocks and report which of them they consider accepted.
// If a majority of the peers report that a block is accepted, then the node
// will consider that block to be accepted by the network. This assumes that a
// majority of the network is correct. If a majority of the network is
// malicious, the node may accept an incorrect block.
type Majority struct {
	requests

	log         logging.Logger
	nodeWeights map[ids.NodeID]uint64

	// received maps the blockID to the total sum of weight that has reported
	// that block as accepted.
	received map[ids.ID]uint64
	accepted []ids.ID
}

func NewMajority(
	log logging.Logger,
	nodeWeights map[ids.NodeID]uint64,
	maxOutstanding int,
) *Majority {
	return &Majority{
		requests: requests{
			maxOutstanding: maxOutstanding,
			pendingSend:    set.Of(maps.Keys(nodeWeights)...),
		},
		log:         log,
		nodeWeights: nodeWeights,
		received:    make(map[ids.ID]uint64),
	}
}

func (m *Majority) RecordOpinion(_ context.Context, nodeID ids.NodeID, blkIDs set.Set[ids.ID]) error {
	if !m.recordResponse(nodeID) {
		// The chain router should have already dropped unexpected messages.
		m.log.Error("received unexpected opinion",
			zap.String("pollType", "majority"),
			zap.Stringer("nodeID", nodeID),
			zap.Reflect("blkIDs", blkIDs),
		)
		return nil
	}

	weight := m.nodeWeights[nodeID]
	for blkID := range blkIDs {
		newWeight, err := math.Add(m.received[blkID], weight)
		if err != nil {
			return err
		}
		m.received[blkID] = newWeight
	}

	if !m.finished() {
		return nil
	}

	var (
		totalWeight uint64
		err         error
	)
	for _, weight := range m.nodeWeights {
		totalWeight, err = math.Add(totalWeight, weight)
		if err != nil {
			return err
		}
	}

	requiredWeight := totalWeight/2 + 1
	for blkID, weight := range m.received {
		if weight >= requiredWeight {
			m.accepted = append(m.accepted, blkID)
		}
	}

	m.log.Debug("finalized bootstrapping poll",
		zap.String("pollType", "majority"),
		zap.Stringers("accepted", m.accepted),
	)
	return nil
}

func (m *Majority) Result(context.Context) ([]ids.ID, bool) {
	return m.accepted, m.finished()
}
