// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
)

type decoratedEngineWithStragglerDetector struct {
	*Engine
	sd *stragglerDetector
	f  func(time.Duration)
}

func NewDecoratedEngineWithStragglerDetector(e *Engine, time func() time.Time, f func(time.Duration)) *decoratedEngineWithStragglerDetector {
	minConfRatio := float64(e.Params.AlphaConfidence) / float64(e.Params.K)

	subnet := e.Ctx.SubnetID

	sa := &snapshotAnalyzer{
		lastAcceptedHeight: onlyHeight(e.Consensus.LastAccepted),
		log:                e.Config.Ctx.Log,
	}

	s := &snapshotter{
		totalWeight: func() (uint64, error) {
			return e.Validators.TotalWeight(subnet)
		},
		log:                        e.Config.Ctx.Log,
		connectedValidators:        e.Config.ConnectedValidators.ConnectedValidators,
		minConfirmationThreshold:   minConfRatio,
		lastAcceptedHeightByNodeID: e.acceptedFrontiers.LastAccepted,
	}

	conf := stragglerDetectorConfig{
		getSnapshot:               s.getNetworkSnapshot,
		areWeBehindTheRest:        sa.areWeBehindTheRest,
		minStragglerCheckInterval: minStragglerCheckInterval,
		log:                       e.Config.Ctx.Log,
		getTime:                   time,
	}

	sd := newStragglerDetector(conf)

	return &decoratedEngineWithStragglerDetector{
		Engine: e,
		f:      f,
		sd:     sd,
	}
}

func (de *decoratedEngineWithStragglerDetector) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID, acceptedHeight uint64) error {
	behindDuration := de.sd.CheckIfWeAreStragglingBehind()
	if behindDuration > 0 {
		de.Engine.Config.Ctx.Log.Info("We are behind the rest of the network", zap.Float64("seconds", behindDuration.Seconds()))
	}
	de.Engine.metrics.stragglingDuration.Set(float64(behindDuration))
	de.f(behindDuration)
	return de.Engine.Chits(ctx, nodeID, requestID, preferredID, preferredIDAtHeight, acceptedID, acceptedHeight)
}
