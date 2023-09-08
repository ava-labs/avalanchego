// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ validators.SetCallbackListener = (*GossipTrackerCallback)(nil)

// GossipTrackerCallback synchronizes GossipTracker's validator state with the
// validator set it's registered to.
type GossipTrackerCallback struct {
	Log           logging.Logger
	GossipTracker GossipTracker
}

// OnValidatorAdded adds [validatorID] to the set of validators that can be
// gossiped about
func (g *GossipTrackerCallback) OnValidatorAdded(
	genericNodeID ids.GenericNodeID,
	_ *bls.PublicKey,
	txID ids.ID,
	_ uint64,
) {
	nodeID, err := ids.NodeIDFromGenericNodeID(genericNodeID)
	if err != nil {
		panic(err)
	}
	vdr := ValidatorID{
		NodeID: nodeID,
		TxID:   txID,
	}
	if !g.GossipTracker.AddValidator(vdr) {
		g.Log.Error("failed to add a validator",
			zap.Stringer("nodeID", &genericNodeID),
			zap.Stringer("txID", txID),
		)
	}
}

// OnValidatorRemoved removes [validatorID] from the set of validators that can
// be gossiped about.
func (g *GossipTrackerCallback) OnValidatorRemoved(genericNodeID ids.GenericNodeID, _ uint64) {
	nodeID, err := ids.NodeIDFromGenericNodeID(genericNodeID)
	if err != nil {
		panic(err)
	}
	if !g.GossipTracker.RemoveValidator(nodeID) {
		g.Log.Error("failed to remove a validator",
			zap.Stringer("nodeID", nodeID),
		)
	}
}

// OnValidatorWeightChanged does nothing because PeerList gossip doesn't care
// about validator weights.
func (*GossipTrackerCallback) OnValidatorWeightChanged(ids.GenericNodeID, uint64, uint64) {}
