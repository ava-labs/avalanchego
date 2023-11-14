// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"

	"go.uber.org/zap"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/sampler"
	"github.com/ava-labs/avalanchego/utils/set"
)

type majority struct {
	log            logging.Logger
	nodeWeights    map[ids.NodeID]uint64
	maxOutstanding int

	pendingSendAcceptedFrontier set.Set[ids.NodeID]
	outstandingAcceptedFrontier set.Set[ids.NodeID]
	receivedAcceptedFrontierSet set.Set[ids.ID]
	receivedAcceptedFrontier    []ids.ID

	pendingSendAccepted set.Set[ids.NodeID]
	outstandingAccepted set.Set[ids.NodeID]
	receivedAccepted    map[ids.ID]uint64
	accepted            []ids.ID
}

func New(
	log logging.Logger,
	nodeWeights map[ids.NodeID]uint64,
	maxFrontiers int,
	maxOutstanding int,
) (Bootstrapper, error) {
	nodeIDs := maps.Keys(nodeWeights)
	m := &majority{
		log:                 log,
		nodeWeights:         nodeWeights,
		maxOutstanding:      maxOutstanding,
		pendingSendAccepted: set.Of(nodeIDs...),
		receivedAccepted:    make(map[ids.ID]uint64),
	}

	maxFrontiers = math.Min(maxFrontiers, len(nodeIDs))
	sampler := sampler.NewUniform()
	sampler.Initialize(uint64(len(nodeIDs)))
	indicies, err := sampler.Sample(maxFrontiers)
	for _, index := range indicies {
		m.pendingSendAcceptedFrontier.Add(nodeIDs[index])
	}

	log.Debug("sampled nodes to seed bootstrapping frontier",
		zap.Reflect("sampledNodes", m.pendingSendAcceptedFrontier),
		zap.Int("numNodes", len(nodeIDs)),
	)

	return m, err
}

func (m *majority) GetAcceptedFrontiersToSend(context.Context) set.Set[ids.NodeID] {
	return getPeersToSend(
		&m.pendingSendAcceptedFrontier,
		&m.outstandingAcceptedFrontier,
		m.maxOutstanding,
	)
}

func (m *majority) RecordAcceptedFrontier(_ context.Context, nodeID ids.NodeID, blkIDs ...ids.ID) {
	if !m.outstandingAcceptedFrontier.Contains(nodeID) {
		// The chain router should have already dropped unexpected messages.
		m.log.Error("received unexpected message",
			zap.Stringer("messageOp", message.AcceptedFrontierOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringers("blkIDs", blkIDs),
		)
		return
	}

	m.outstandingAcceptedFrontier.Remove(nodeID)
	m.receivedAcceptedFrontierSet.Add(blkIDs...)

	if !m.finishedFetchingAcceptedFrontiers() {
		return
	}

	m.receivedAcceptedFrontier = m.receivedAcceptedFrontierSet.List()

	m.log.Debug("finalized bootstrapping frontier",
		zap.Stringers("frontier", m.receivedAcceptedFrontier),
	)
}

func (m *majority) GetAcceptedFrontier(context.Context) ([]ids.ID, bool) {
	return m.receivedAcceptedFrontier, m.finishedFetchingAcceptedFrontiers()
}

func (m *majority) GetAcceptedToSend(context.Context) set.Set[ids.NodeID] {
	if !m.finishedFetchingAcceptedFrontiers() {
		return nil
	}

	return getPeersToSend(
		&m.pendingSendAccepted,
		&m.outstandingAccepted,
		m.maxOutstanding,
	)
}

func (m *majority) RecordAccepted(_ context.Context, nodeID ids.NodeID, blkIDs []ids.ID) error {
	if !m.outstandingAccepted.Contains(nodeID) {
		// The chain router should have already dropped unexpected messages.
		m.log.Error("received unexpected message",
			zap.Stringer("messageOp", message.AcceptedOp),
			zap.Stringer("nodeID", nodeID),
			zap.Stringers("blkIDs", blkIDs),
		)
		return nil
	}

	m.outstandingAccepted.Remove(nodeID)

	weight := m.nodeWeights[nodeID]
	for _, blkID := range blkIDs {
		newWeight, err := math.Add64(m.receivedAccepted[blkID], weight)
		if err != nil {
			return err
		}
		m.receivedAccepted[blkID] = newWeight
	}

	if !m.finishedFetchingAccepted() {
		return nil
	}

	var (
		totalWeight uint64
		err         error
	)
	for _, weight := range m.nodeWeights {
		totalWeight, err = math.Add64(totalWeight, weight)
		if err != nil {
			return err
		}
	}

	requiredWeight := totalWeight/2 + 1
	for blkID, weight := range m.receivedAccepted {
		if weight >= requiredWeight {
			m.accepted = append(m.accepted, blkID)
		}
	}

	m.log.Debug("finalized bootstrapping instance",
		zap.Stringers("accepted", m.accepted),
	)
	return nil
}

func (m *majority) GetAccepted(context.Context) ([]ids.ID, bool) {
	return m.accepted, m.finishedFetchingAccepted()
}

func (m *majority) finishedFetchingAcceptedFrontiers() bool {
	return m.pendingSendAcceptedFrontier.Len() == 0 &&
		m.outstandingAcceptedFrontier.Len() == 0
}

func (m *majority) finishedFetchingAccepted() bool {
	return m.pendingSendAccepted.Len() == 0 &&
		m.outstandingAccepted.Len() == 0
}

func getPeersToSend(pendingSend, outstanding *set.Set[ids.NodeID], maxOutstanding int) set.Set[ids.NodeID] {
	numPending := outstanding.Len()
	if numPending >= maxOutstanding {
		return nil
	}

	numToSend := math.Min(
		maxOutstanding-numPending,
		pendingSend.Len(),
	)
	nodeIDs := set.NewSet[ids.NodeID](numToSend)
	for i := 0; i < numToSend; i++ {
		nodeID, _ := pendingSend.Pop()
		nodeIDs.Add(nodeID)
	}
	outstanding.Union(nodeIDs)
	return nodeIDs
}
