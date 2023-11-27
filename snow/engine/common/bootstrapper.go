// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"

	smbootstrapper "github.com/ava-labs/avalanchego/snow/consensus/snowman/bootstrapper"
)

const (
	// StatusUpdateFrequency is how many containers should be processed between
	// logs
	StatusUpdateFrequency = 5000

	// MaxOutstandingBroadcastRequests is the maximum number of requests to have
	// outstanding when broadcasting.
	MaxOutstandingBroadcastRequests = 50
)

var _ Bootstrapper = (*bootstrapper)(nil)

type Bootstrapper interface {
	AcceptedFrontierHandler
	AcceptedHandler
	Haltable
	Startup(context.Context) error
	Restart(ctx context.Context) error
}

// It collects mechanisms common to both snowman and avalanche bootstrappers
type bootstrapper struct {
	Config
	Halter

	minority smbootstrapper.Poll
	majority smbootstrapper.Poll
}

func NewCommonBootstrapper(config Config) Bootstrapper {
	return &bootstrapper{
		Config:   config,
		minority: smbootstrapper.Noop,
		majority: smbootstrapper.Noop,
	}
}

func (b *bootstrapper) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerID ids.ID) error {
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("received out-of-sync AcceptedFrontier message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.Config.SharedCfg.RequestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if err := b.minority.RecordOpinion(ctx, nodeID, set.Of(containerID)); err != nil {
		return err
	}
	return b.sendMessagesOrFinish(ctx)
}

func (b *bootstrapper) GetAcceptedFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("received out-of-sync GetAcceptedFrontierFailed message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.Config.SharedCfg.RequestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if err := b.minority.RecordOpinion(ctx, nodeID, nil); err != nil {
		return err
	}
	return b.sendMessagesOrFinish(ctx)
}

func (b *bootstrapper) Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs set.Set[ids.ID]) error {
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("received out-of-sync Accepted message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.Config.SharedCfg.RequestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if err := b.majority.RecordOpinion(ctx, nodeID, containerIDs); err != nil {
		return err
	}
	return b.sendMessagesOrFinish(ctx)
}

func (b *bootstrapper) GetAcceptedFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("received out-of-sync GetAcceptedFailed message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.Config.SharedCfg.RequestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if err := b.majority.RecordOpinion(ctx, nodeID, nil); err != nil {
		return err
	}
	return b.sendMessagesOrFinish(ctx)
}

func (b *bootstrapper) Startup(ctx context.Context) error {
	currentBeacons := b.Beacons.GetMap(b.Ctx.SubnetID)
	nodeWeights := make(map[ids.NodeID]uint64, len(currentBeacons))
	for nodeID, beacon := range currentBeacons {
		nodeWeights[nodeID] = beacon.Weight
	}

	frontierNodes, err := smbootstrapper.Sample(nodeWeights, b.SampleK)
	if err != nil {
		return err
	}

	b.Ctx.Log.Debug("sampled nodes to seed bootstrapping frontier",
		zap.Reflect("sampledNodes", frontierNodes),
		zap.Int("numNodes", len(nodeWeights)),
	)

	b.minority = smbootstrapper.NewMinority(
		b.Ctx.Log,
		frontierNodes,
		MaxOutstandingBroadcastRequests,
	)
	b.majority = smbootstrapper.NewMajority(
		b.Ctx.Log,
		nodeWeights,
		MaxOutstandingBroadcastRequests,
	)

	if accepted, finalized := b.majority.Result(ctx); finalized {
		b.Ctx.Log.Info("bootstrapping skipped",
			zap.String("reason", "no provided bootstraps"),
		)
		return b.Bootstrapable.ForceAccepted(ctx, accepted)
	}

	b.Config.SharedCfg.RequestID++
	return b.sendMessagesOrFinish(ctx)
}

func (b *bootstrapper) Restart(ctx context.Context) error {
	b.Ctx.Log.Debug("Checking for new frontiers")
	b.Config.SharedCfg.Restarted = true
	return b.Startup(ctx)
}

func (b *bootstrapper) sendMessagesOrFinish(ctx context.Context) error {
	if peers := b.minority.GetPeers(ctx); peers.Len() > 0 {
		b.Sender.SendGetAcceptedFrontier(ctx, peers, b.Config.SharedCfg.RequestID)
		return nil
	}

	potentialAccepted, finalized := b.minority.Result(ctx)
	if !finalized {
		// We haven't finalized the accepted frontier, so we should wait for the
		// outstanding requests.
		return nil
	}

	if peers := b.majority.GetPeers(ctx); peers.Len() > 0 {
		b.Sender.SendGetAccepted(ctx, peers, b.Config.SharedCfg.RequestID, potentialAccepted)
		return nil
	}

	accepted, finalized := b.majority.Result(ctx)
	if !finalized {
		// We haven't finalized the accepted set, so we should wait for the
		// outstanding requests.
		return nil
	}

	numAccepted := len(accepted)
	if numAccepted == 0 {
		b.Ctx.Log.Debug("restarting bootstrap",
			zap.String("reason", "no blocks accepted"),
			zap.Int("numBeacons", b.Beacons.Count(b.Ctx.SubnetID)),
		)
		return b.Startup(ctx)
	}

	if !b.Config.SharedCfg.Restarted {
		b.Ctx.Log.Info("bootstrapping started syncing",
			zap.Int("numAccepted", numAccepted),
		)
	} else {
		b.Ctx.Log.Debug("bootstrapping started syncing",
			zap.Int("numAccepted", numAccepted),
		)
	}

	return b.Bootstrapable.ForceAccepted(ctx, accepted)
}
