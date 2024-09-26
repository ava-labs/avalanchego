// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

type decoratedEngineWithStragglerDetector struct {
	*Engine
	sd *stragglerDetector
	f  func(time.Duration)
}

func NewDecoratedEngineWithStragglerDetector(e *Engine, time func() time.Time, f func(time.Duration)) common.Engine {
	minConfRatio := float64(e.Params.AlphaConfidence) / float64(e.Params.K)
	sd := newStragglerDetector(time, e.Config.Ctx.Log, minConfRatio, e.Consensus.LastAccepted,
		e.Config.ConnectedValidators.ConnectedValidators, e.Config.ConnectedValidators.ConnectedPercent,
		e.Consensus.Processing, e.acceptedFrontiers.LastAccepted)
	return &decoratedEngineWithStragglerDetector{
		Engine: e,
		f:      f,
		sd:     sd,
	}
}

func (de *decoratedEngineWithStragglerDetector) Chits(ctx context.Context, nodeID ids.NodeID, requestID uint32, preferredID ids.ID, preferredIDAtHeight ids.ID, acceptedID ids.ID) error {
	behindDuration := de.sd.CheckIfWeAreStragglingBehind()
	if behindDuration > 0 {
		de.Engine.Config.Ctx.Log.Info("We are behind the rest of the network", zap.Float64("seconds", behindDuration.Seconds()))
	}
	de.Engine.metrics.stragglingDuration.Set(float64(behindDuration))
	de.f(behindDuration)
	return de.Engine.Chits(ctx, nodeID, requestID, preferredID, preferredIDAtHeight, acceptedID)
}
