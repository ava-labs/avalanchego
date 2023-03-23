// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/types"
)

var _ validators.SetCallbackListener = (*logger)(nil)

type logger struct {
	log      logging.Logger
	vmState  *utils.Atomic[snow.State]
	subnetID ids.ID
	nodeIDs  set.Set[ids.NodeID]
}

// newValidatorsLogger returns a callback listener that will log validator set changes for
// the specified validators
func newValidatorsLogger(
	log logging.Logger,
	vmState *utils.Atomic[snow.State],
	subnetID ids.ID,
	nodeIDs ...ids.NodeID,
) validators.SetCallbackListener {
	nodeIDSet := set.NewSet[ids.NodeID](len(nodeIDs))
	nodeIDSet.Add(nodeIDs...)
	return &logger{
		log:      log,
		vmState:  vmState,
		subnetID: subnetID,
		nodeIDs:  nodeIDSet,
	}
}

func (l *logger) OnValidatorAdded(
	nodeID ids.NodeID,
	pk *bls.PublicKey,
	txID ids.ID,
	weight uint64,
) {
	if status.DoneBootstraping(l.vmState.Get()) && l.nodeIDs.Contains(nodeID) {
		var pkBytes []byte
		if pk != nil {
			pkBytes = bls.PublicKeyToBytes(pk)
		}
		l.log.Info("node added to validator set",
			zap.Stringer("subnetID", l.subnetID),
			zap.Stringer("nodeID", nodeID),
			zap.Reflect("publicKey", types.JSONByteSlice(pkBytes)),
			zap.Stringer("txID", txID),
			zap.Uint64("weight", weight),
		)
	}
}

func (l *logger) OnValidatorRemoved(
	nodeID ids.NodeID,
	weight uint64,
) {
	if status.DoneBootstraping(l.vmState.Get()) && l.nodeIDs.Contains(nodeID) {
		l.log.Info("node removed from validator set",
			zap.Stringer("subnetID", l.subnetID),
			zap.Stringer("nodeID", nodeID),
			zap.Uint64("weight", weight),
		)
	}
}

func (l *logger) OnValidatorWeightChanged(
	nodeID ids.NodeID,
	oldWeight uint64,
	newWeight uint64,
) {
	if status.DoneBootstraping(l.vmState.Get()) && l.nodeIDs.Contains(nodeID) {
		l.log.Info("validator weight changed",
			zap.Stringer("subnetID", l.subnetID),
			zap.Stringer("nodeID", nodeID),
			zap.Uint64("previousWeight ", oldWeight),
			zap.Uint64("newWeight ", newWeight),
		)
	}
}
