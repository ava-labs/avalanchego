// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

var _ NodeStakeTracker = (*logger)(nil)

type NodeStakeTracker SetCallbackListener

// logger allows tracking changes related to a specified validator
type logger struct {
	subnetID ids.ID
	nodeID   ids.NodeID
	log      logging.Logger
}

func NewLogger(subnetID ids.ID, targetNode ids.NodeID, log logging.Logger) NodeStakeTracker {
	return &logger{
		subnetID: subnetID,
		nodeID:   targetNode,
		log:      log,
	}
}

func (l *logger) OnValidatorAdded(valID ids.NodeID, _ *bls.PublicKey, txID ids.ID, weight uint64) {
	if valID == l.nodeID {
		l.log.Info("tracking validator: validator added to validators set",
			zap.Stringer("subnetID ", l.subnetID),
			zap.Stringer("nodeID ", valID),
			zap.Uint64("weight ", weight),
			zap.Stringer("txID", txID),
		)
	}
}

func (l *logger) OnValidatorRemoved(valID ids.NodeID, weight uint64) {
	if valID == l.nodeID {
		l.log.Info("tracking validator: validator dropped from validators set",
			zap.Stringer("subnetID ", l.subnetID),
			zap.Stringer("nodeID ", valID),
			zap.Uint64("weight ", weight),
		)
	}
}

func (l *logger) OnValidatorWeightChanged(valID ids.NodeID, oldWeight, newWeight uint64) {
	if valID == l.nodeID {
		l.log.Info("tracking validator: validator weight change",
			zap.Stringer("subnetID ", l.subnetID),
			zap.Stringer("nodeID ", valID),
			zap.Uint64("previous weight ", oldWeight),
			zap.Uint64("current weight ", newWeight),
		)
	}
}
