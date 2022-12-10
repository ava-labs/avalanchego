// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"context"

	stdmath "math"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
)

const (
	// StatusUpdateFrequency is how many containers should be processed between
	// logs
	StatusUpdateFrequency = 5000

	// MaxOutstandingGetAncestorsRequests is the maximum number of GetAncestors
	// sent but not responded to/failed
	MaxOutstandingGetAncestorsRequests = 10

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
	Restart(ctx context.Context, reset bool) error
}

// It collects mechanisms common to both snowman and avalanche bootstrappers
type bootstrapper struct {
	Config
	Halter

	// Holds the beacons that were sampled for the accepted frontier
	sampledBeacons validators.Set
	// IDs of validators we should request an accepted frontier from
	pendingSendAcceptedFrontier set.Set[ids.NodeID]
	// IDs of validators we requested an accepted frontier from but haven't
	// received a reply yet
	pendingReceiveAcceptedFrontier set.Set[ids.NodeID]
	// IDs of validators that failed to respond with their accepted frontier
	failedAcceptedFrontier set.Set[ids.NodeID]
	// IDs of all the returned accepted frontiers
	acceptedFrontierSet set.Set[ids.ID]

	// IDs of validators we should request filtering the accepted frontier from
	pendingSendAccepted set.Set[ids.NodeID]
	// IDs of validators we requested filtering the accepted frontier from but
	// haven't received a reply yet
	pendingReceiveAccepted set.Set[ids.NodeID]
	// IDs of validators that failed to respond with their filtered accepted
	// frontier
	failedAccepted set.Set[ids.NodeID]
	// IDs of the returned accepted containers and the stake weight that has
	// marked them as accepted
	acceptedVotes    map[ids.ID]uint64
	acceptedFrontier []ids.ID

	// number of times the bootstrap has been attempted
	bootstrapAttempts int
}

func NewCommonBootstrapper(config Config) Bootstrapper {
	return &bootstrapper{
		Config: config,
	}
}

func (b *bootstrapper) AcceptedFrontier(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
	// ignores any late responses
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("received out-of-sync AcceptedFrontier message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.Config.SharedCfg.RequestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if !b.pendingReceiveAcceptedFrontier.Contains(nodeID) {
		b.Ctx.Log.Debug("received unexpected AcceptedFrontier message",
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}

	// Mark that we received a response from [nodeID]
	b.pendingReceiveAcceptedFrontier.Remove(nodeID)

	// Union the reported accepted frontier from [nodeID] with the accepted
	// frontier we got from others
	b.acceptedFrontierSet.Add(containerIDs...)

	b.sendGetAcceptedFrontiers(ctx)

	// still waiting on requests
	if b.pendingReceiveAcceptedFrontier.Len() != 0 {
		return nil
	}

	// We've received the accepted frontier from every bootstrap validator
	// Ask each bootstrap validator to filter the list of containers that we were
	// told are on the accepted frontier such that the list only contains containers
	// they think are accepted.
	//
	// Create a newAlpha taking using the sampled beacon
	// Keep the proportion of b.Alpha in the newAlpha
	// newAlpha := totalSampledWeight * b.Alpha / totalWeight

	newAlpha := float64(b.sampledBeacons.Weight()*b.Alpha) / float64(b.Beacons.Weight())

	failedBeaconWeight := b.Beacons.SubsetWeight(b.failedAcceptedFrontier)

	// fail the bootstrap if the weight is not enough to bootstrap
	if float64(b.sampledBeacons.Weight())-newAlpha < float64(failedBeaconWeight) {
		if b.Config.RetryBootstrap {
			b.Ctx.Log.Debug("restarting bootstrap",
				zap.String("reason", "not enough frontiers received"),
				zap.Int("numBeacons", b.Beacons.Len()),
				zap.Int("numFailedBootstrappers", b.failedAcceptedFrontier.Len()),
				zap.Int("numBootstrapAttemps", b.bootstrapAttempts),
			)
			return b.Restart(ctx, false)
		}

		b.Ctx.Log.Debug("didn't receive enough frontiers",
			zap.Int("numFailedValidators", b.failedAcceptedFrontier.Len()),
			zap.Int("numBootstrapAttempts", b.bootstrapAttempts),
		)
	}

	b.Config.SharedCfg.RequestID++
	b.acceptedFrontier = b.acceptedFrontierSet.List()

	b.sendGetAccepted(ctx)
	return nil
}

func (b *bootstrapper) GetAcceptedFrontierFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// ignores any late responses
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("received out-of-sync GetAcceptedFrontierFailed message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.Config.SharedCfg.RequestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	// If we can't get a response from [nodeID], act as though they said their
	// accepted frontier is empty and we add the validator to the failed list
	b.failedAcceptedFrontier.Add(nodeID)
	return b.AcceptedFrontier(ctx, nodeID, requestID, nil)
}

func (b *bootstrapper) Accepted(ctx context.Context, nodeID ids.NodeID, requestID uint32, containerIDs []ids.ID) error {
	// ignores any late responses
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("received out-of-sync Accepted message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.Config.SharedCfg.RequestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	if !b.pendingReceiveAccepted.Contains(nodeID) {
		b.Ctx.Log.Debug("received unexpected Accepted message",
			zap.Stringer("nodeID", nodeID),
		)
		return nil
	}
	// Mark that we received a response from [nodeID]
	b.pendingReceiveAccepted.Remove(nodeID)

	weight := b.Beacons.GetWeight(nodeID)
	for _, containerID := range containerIDs {
		previousWeight := b.acceptedVotes[containerID]
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			b.Ctx.Log.Error("failed calculating the Accepted votes",
				zap.Uint64("weight", weight),
				zap.Uint64("previousWeight", previousWeight),
				zap.Error(err),
			)
			newWeight = stdmath.MaxUint64
		}
		b.acceptedVotes[containerID] = newWeight
	}

	b.sendGetAccepted(ctx)

	// wait on pending responses
	if b.pendingReceiveAccepted.Len() != 0 {
		return nil
	}

	// We've received the filtered accepted frontier from every bootstrap validator
	// Accept all containers that have a sufficient weight behind them
	accepted := make([]ids.ID, 0, len(b.acceptedVotes))
	for containerID, weight := range b.acceptedVotes {
		if weight >= b.Alpha {
			accepted = append(accepted, containerID)
		}
	}

	// if we don't have enough weight for the bootstrap to be accepted then retry or fail the bootstrap
	size := len(accepted)
	if size == 0 && b.Beacons.Len() > 0 {
		// retry the bootstrap if the weight is not enough to bootstrap
		failedBeaconWeight := b.Beacons.SubsetWeight(b.failedAccepted)

		// in a zero network there will be no accepted votes but the voting weight will be greater than the failed weight
		if b.Config.RetryBootstrap && b.Beacons.Weight()-b.Alpha < failedBeaconWeight {
			b.Ctx.Log.Debug("restarting bootstrap",
				zap.String("reason", "not enough votes received"),
				zap.Int("numBeacons", b.Beacons.Len()),
				zap.Int("numFailedBootstrappers", b.failedAccepted.Len()),
				zap.Int("numBootstrapAttempts", b.bootstrapAttempts),
			)
			return b.Restart(ctx, false)
		}
	}

	if !b.Config.SharedCfg.Restarted {
		b.Ctx.Log.Info("bootstrapping started syncing",
			zap.Int("numVerticesInFrontier", size),
		)
	} else {
		b.Ctx.Log.Debug("bootstrapping started syncing",
			zap.Int("numVerticesInFrontier", size),
		)
	}

	return b.Bootstrapable.ForceAccepted(ctx, accepted)
}

func (b *bootstrapper) GetAcceptedFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	// ignores any late responses
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("received out-of-sync GetAcceptedFailed message",
			zap.Stringer("nodeID", nodeID),
			zap.Uint32("expectedRequestID", b.Config.SharedCfg.RequestID),
			zap.Uint32("requestID", requestID),
		)
		return nil
	}

	// If we can't get a response from [nodeID], act as though they said that
	// they think none of the containers we sent them in GetAccepted are
	// accepted
	b.failedAccepted.Add(nodeID)
	return b.Accepted(ctx, nodeID, requestID, nil)
}

func (b *bootstrapper) Startup(ctx context.Context) error {
	beaconIDs, err := b.Beacons.Sample(b.Config.SampleK)
	if err != nil {
		return err
	}

	b.sampledBeacons = validators.NewSet()
	b.pendingSendAcceptedFrontier.Clear()
	for _, nodeID := range beaconIDs {
		if !b.sampledBeacons.Contains(nodeID) {
			// Invariant: We never use the TxID or BLS keys populated here.
			err = b.sampledBeacons.Add(nodeID, nil, ids.Empty, 1)
		} else {
			err = b.sampledBeacons.AddWeight(nodeID, 1)
		}
		if err != nil {
			return err
		}
		b.pendingSendAcceptedFrontier.Add(nodeID)
	}

	b.pendingReceiveAcceptedFrontier.Clear()
	b.failedAcceptedFrontier.Clear()
	b.acceptedFrontierSet.Clear()

	b.pendingSendAccepted.Clear()
	for _, vdr := range b.Beacons.List() {
		b.pendingSendAccepted.Add(vdr.NodeID)
	}

	b.pendingReceiveAccepted.Clear()
	b.failedAccepted.Clear()
	b.acceptedVotes = make(map[ids.ID]uint64)

	b.bootstrapAttempts++
	if b.pendingSendAcceptedFrontier.Len() == 0 {
		b.Ctx.Log.Info("bootstrapping skipped",
			zap.String("reason", "no provided bootstraps"),
		)
		return b.Bootstrapable.ForceAccepted(ctx, nil)
	}

	b.Config.SharedCfg.RequestID++
	b.sendGetAcceptedFrontiers(ctx)
	return nil
}

func (b *bootstrapper) Restart(ctx context.Context, reset bool) error {
	// resets the attempts when we're pulling blocks/vertices we don't want to
	// fail the bootstrap at that stage
	if reset {
		b.Ctx.Log.Debug("Checking for new frontiers")

		b.Config.SharedCfg.Restarted = true
		b.bootstrapAttempts = 0
	}

	if b.bootstrapAttempts > 0 && b.bootstrapAttempts%b.RetryBootstrapWarnFrequency == 0 {
		b.Ctx.Log.Debug("check internet connection",
			zap.Int("numBootstrapAttempts", b.bootstrapAttempts),
		)
	}

	return b.Startup(ctx)
}

// Ask up to [MaxOutstandingBroadcastRequests] bootstrap validators to send
// their accepted frontier with the current accepted frontier
func (b *bootstrapper) sendGetAcceptedFrontiers(ctx context.Context) {
	vdrs := set.NewSet[ids.NodeID](1)
	for b.pendingSendAcceptedFrontier.Len() > 0 && b.pendingReceiveAcceptedFrontier.Len() < MaxOutstandingBroadcastRequests {
		vdr, _ := b.pendingSendAcceptedFrontier.Pop()
		// Add the validator to the set to send the messages to
		vdrs.Add(vdr)
		// Add the validator to send pending receipt set
		b.pendingReceiveAcceptedFrontier.Add(vdr)
	}

	if vdrs.Len() > 0 {
		b.Sender.SendGetAcceptedFrontier(ctx, vdrs, b.Config.SharedCfg.RequestID)
	}
}

// Ask up to [MaxOutstandingBroadcastRequests] bootstrap validators to send
// their filtered accepted frontier
func (b *bootstrapper) sendGetAccepted(ctx context.Context) {
	vdrs := set.NewSet[ids.NodeID](1)
	for b.pendingSendAccepted.Len() > 0 && b.pendingReceiveAccepted.Len() < MaxOutstandingBroadcastRequests {
		vdr, _ := b.pendingSendAccepted.Pop()
		// Add the validator to the set to send the messages to
		vdrs.Add(vdr)
		// Add the validator to send pending receipt set
		b.pendingReceiveAccepted.Add(vdr)
	}

	if vdrs.Len() > 0 {
		b.Ctx.Log.Debug("sent GetAccepted messages",
			zap.Int("numSent", vdrs.Len()),
			zap.Int("numPending", b.pendingSendAccepted.Len()),
		)
		b.Sender.SendGetAccepted(ctx, vdrs, b.Config.SharedCfg.RequestID, b.acceptedFrontier)
	}
}
