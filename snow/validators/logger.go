// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ NodeStakeLogger = (*logger)(nil)

// NodeStakeLogger allows logging stake changes of a specified validator
type NodeStakeLogger SetCallbackListener

type logger struct {
	log      logging.Logger
	enabled  *utils.Atomic[bool]
	subnetID ids.ID
	nodeIDs  set.Set[ids.NodeID]
}

func NewLogger(
	log logging.Logger,
	enabled *utils.Atomic[bool],
	subnetID ids.ID,
	nodeIDs ...ids.NodeID,
) NodeStakeLogger {
	nodeIDSet := set.NewSet[ids.NodeID](len(nodeIDs))
	nodeIDSet.Add(nodeIDs...)
	return &logger{
		log:      log,
		enabled:  enabled,
		subnetID: subnetID,
		nodeIDs:  nodeIDSet,
	}
}

func (l *logger) OnValidatorAdded(
	nodeID ids.NodeID,
	_ *bls.PublicKey,
	txID ids.ID,
	weight uint64,
) {
	if l.enabled.Get() && l.nodeIDs.Contains(nodeID) {
		l.log.Info("node added to validator set",
			zap.Stringer("subnetID", l.subnetID),
			zap.Stringer("nodeID", nodeID),
			zap.Stringer("txID", txID),
			zap.Uint64("weight", weight),
		)
	}
}

func (l *logger) OnValidatorRemoved(
	nodeID ids.NodeID,
	weight uint64,
) {
	if l.enabled.Get() && l.nodeIDs.Contains(nodeID) {
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
	if l.enabled.Get() && l.nodeIDs.Contains(nodeID) {
		l.log.Info("validator weight changed",
			zap.Stringer("subnetID", l.subnetID),
			zap.Stringer("nodeID", nodeID),
			zap.Uint64("previousWeight ", oldWeight),
			zap.Uint64("newWeight ", newWeight),
		)
	}
}
