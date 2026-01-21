// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Poll = (*Minority)(nil)

// Minority implements the bootstrapping poll to determine the initial set of
// potentially acceptable blocks.
//
// This poll fetches the last accepted block from an initial set of peers. In
// order for the protocol to find a recently accepted block, there must be at
// least one correct node in this set of peers. If there is not a correct node
// in the set of peers, the node will not accept an incorrect block. However,
// the node may be unable to find an acceptable block.
type Minority struct {
	requests

	log logging.Logger

	receivedSet set.Set[ids.ID]
	received    []ids.ID
}

func NewMinority(
	log logging.Logger,
	frontierNodes set.Set[ids.NodeID],
	maxOutstanding int,
) *Minority {
	return &Minority{
		requests: requests{
			maxOutstanding: maxOutstanding,
			pendingSend:    frontierNodes,
		},
		log: log,
	}
}

func (m *Minority) RecordOpinion(_ context.Context, nodeID ids.NodeID, blkIDs set.Set[ids.ID]) error {
	if !m.recordResponse(nodeID) {
		// The chain router should have already dropped unexpected messages.
		m.log.Error("received unexpected opinion",
			zap.String("pollType", "minority"),
			zap.Stringer("nodeID", nodeID),
			zap.Reflect("blkIDs", blkIDs),
		)
		return nil
	}

	m.receivedSet.Union(blkIDs)

	if !m.finished() {
		return nil
	}

	m.received = m.receivedSet.List()

	m.log.Debug("finalized bootstrapping poll",
		zap.String("pollType", "minority"),
		zap.Stringers("frontier", m.received),
	)
	return nil
}

func (m *Minority) Result(context.Context) ([]ids.ID, bool) {
	return m.received, m.finished()
}
