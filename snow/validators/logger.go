// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"go.uber.org/zap"
)

var _ SetCallbackListener = (*Logger)(nil)

// Logger allows tracking changes related to a specified validator
type Logger struct {
	TargetNode ids.NodeID
	Log        logging.Logger
}

func (l *Logger) OnValidatorAdded(valID ids.NodeID, pk *bls.PublicKey, txID ids.ID, weight uint64) {
	if valID == l.TargetNode {
		l.Log.Info("tracking validator: validator added to validators set",
			zap.Stringer("nodeID ", valID),
			zap.Uint64("weight ", weight),
			zap.Stringer("txID", txID),
		)
	}
}

func (l *Logger) OnValidatorRemoved(valID ids.NodeID, weight uint64) {
	if valID == l.TargetNode {
		l.Log.Info("tracking validator: validator dropped from validators set",
			zap.Stringer("nodeID ", valID),
			zap.Uint64("weight ", weight),
		)
	}
}

func (l *Logger) OnValidatorWeightChanged(valID ids.NodeID, oldWeight, newWeight uint64) {
	if valID == l.TargetNode {
		l.Log.Info("tracking validator: validator weight change",
			zap.Stringer("nodeID ", valID),
			zap.Uint64("previous weight ", oldWeight),
			zap.Uint64("current weight ", newWeight),
		)
	}
}
