// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ NodeStakeLogger = (*logger)(nil)

// NodeStakeLogger allows logging stake changes of a specified validator
type NodeStakeLogger SetCallbackListener

type logger struct {
	subnetID ids.ID
	nodeID   ids.NodeID
	log      logging.Logger
}

func NewLogger(subnetID ids.ID, targetNode ids.NodeID, log logging.Logger) NodeStakeLogger {
	return &logger{
		subnetID: subnetID,
		nodeID:   targetNode,
		log:      log,
	}
}

func (l *logger) OnValidatorAdded(nodeID ids.NodeID, _ *bls.PublicKey, txID ids.ID, weight uint64) {
	if nodeID == l.nodeID {
		l.log.Info("tracking validator: validator added to validators set",
			zap.Stringer("subnetID ", l.subnetID),
			zap.Stringer("nodeID ", nodeID),
			zap.Uint64("weight ", weight),
			zap.Stringer("txID", txID),
		)
	}
}

func (l *logger) OnValidatorRemoved(nodeID ids.NodeID, weight uint64) {
	if nodeID == l.nodeID {
		l.log.Info("tracking validator: validator dropped from validators set",
			zap.Stringer("subnetID ", l.subnetID),
			zap.Stringer("nodeID ", nodeID),
			zap.Uint64("weight ", weight),
		)
	}
}

func (l *logger) OnValidatorWeightChanged(nodeID ids.NodeID, oldWeight, newWeight uint64) {
	if nodeID == l.nodeID {
		l.log.Info("tracking validator: validator weight change",
			zap.Stringer("subnetID ", l.subnetID),
			zap.Stringer("nodeID ", nodeID),
			zap.Uint64("previous weight ", oldWeight),
			zap.Uint64("current weight ", newWeight),
		)
	}
}
