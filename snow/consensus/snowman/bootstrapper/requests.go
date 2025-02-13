// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

type requests struct {
	maxOutstanding int

	pendingSend set.Set[ids.NodeID]
	outstanding set.Set[ids.NodeID]
}

func (r *requests) GetPeers(context.Context) set.Set[ids.NodeID] {
	numPending := r.outstanding.Len()
	if numPending >= r.maxOutstanding {
		return nil
	}

	numToSend := min(
		r.maxOutstanding-numPending,
		r.pendingSend.Len(),
	)
	nodeIDs := set.NewSet[ids.NodeID](numToSend)
	for i := 0; i < numToSend; i++ {
		nodeID, _ := r.pendingSend.Pop()
		nodeIDs.Add(nodeID)
	}
	r.outstanding.Union(nodeIDs)
	return nodeIDs
}

func (r *requests) recordResponse(nodeID ids.NodeID) bool {
	wasOutstanding := r.outstanding.Contains(nodeID)
	r.outstanding.Remove(nodeID)
	return wasOutstanding
}

func (r *requests) finished() bool {
	return r.pendingSend.Len() == 0 && r.outstanding.Len() == 0
}
