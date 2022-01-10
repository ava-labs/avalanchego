// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"

	stdmath "math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
)

const (
	// StatusUpdateFrequency is how many containers should be processed between
	// logs
	StatusUpdateFrequency = 5000

	// MaxOutstandingGetAncestorsRequests is the maximum number of GetAncestors
	// sent but not responded to/failed
	MaxOutstandingGetAncestorsRequests = 10

	// MaxOutstandingBootstrapRequests is the maximum number of
	// GetAcceptedFrontier and GetAccepted messages sent but not responded
	// to/failed
	MaxOutstandingBootstrapRequests = 50

	// MaxTimeFetchingAncestors is the maximum amount of time to spend fetching
	// vertices during a call to GetAncestors
	MaxTimeFetchingAncestors = 50 * time.Millisecond
)

var _ Bootstrapper = &bootstrapper{}

type Bootstrapper interface {
	AcceptedFrontierHandler
	AcceptedHandler
	Haltable
	Startup() error
	Restart(reset bool) error
}

// bootstrapper implements the Handler interface.
// It collects mechanisms common to both snowman and avalanche bootstrappers
type bootstrapper struct {
	Config
	Halter

	gR GearRequester
	// Holds the beacons that were sampled for the accepted frontier
	sampledBeacons validators.Set
	// IDs of all the returned accepted frontiers
	acceptedFrontierSet ids.Set
	// IDs of the returned accepted containers and the stake weight that has
	// marked them as accepted
	acceptedVotes    map[ids.ID]uint64
	acceptedFrontier []ids.ID

	// True if RestartBootstrap has been called at least once
	Restarted bool

	// number of times the bootstrap has been attempted
	bootstrapAttempts int
}

func NewCommonBootstrapper(config Config) Bootstrapper {
	return &bootstrapper{
		Config: config,
		gR: NewGearRequester(
			config.Ctx.Log,
			[]message.Op{
				message.AcceptedFrontier,
				message.Accepted,
			}),
	}
}

// AcceptedFrontier implements the AcceptedFrontierHandler interface.
func (b *bootstrapper) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	// ignores any late responses
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("Received an Out-of-Sync AcceptedFrontier - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, b.Config.SharedCfg.RequestID, requestID)
		return nil
	}

	if !b.gR.ConsumeRequested(message.AcceptedFrontier, validatorID) {
		return nil
	}

	b.acceptedFrontierSet.Add(containerIDs...)
	if err := b.sendGetAcceptedFrontiers(); err != nil {
		return err
	}

	// still waiting on requests
	if b.gR.CountRequested(message.AcceptedFrontier) != 0 {
		return nil
	}

	// We've received the accepted frontier from every bootstrap validator
	// Ask each bootstrap validator to filter the list of containers that we were
	// told are on the accepted frontier such that the list only contains containers
	// they think are accepted
	var err error

	// Create a newAlpha taking using the sampled beacon
	// Keep the proportion of b.Alpha in the newAlpha
	// newAlpha := totalSampledWeight * b.Alpha / totalWeight

	newAlpha := float64(b.sampledBeacons.Weight()*b.Alpha) / float64(b.Beacons.Weight())

	failedAcceptedFrontier := b.gR.GetAllFailed(message.AcceptedFrontier)
	failedBeaconWeight, err := b.Beacons.SubsetWeight(failedAcceptedFrontier)
	if err != nil {
		return err
	}

	// fail the bootstrap if the weight is not enough to bootstrap
	if float64(b.sampledBeacons.Weight())-newAlpha < float64(failedBeaconWeight) {
		if b.Config.RetryBootstrap {
			b.Ctx.Log.Debug("Not enough frontiers received, restarting bootstrap... - Beacons: %d - Failed Bootstrappers: %d "+
				"- bootstrap attempt: %d", b.Beacons.Len(), failedAcceptedFrontier.Len(), b.bootstrapAttempts)
			return b.Restart(false)
		}

		b.Ctx.Log.Debug("Didn't receive enough frontiers - failed validators: %d, "+
			"bootstrap attempt: %d", failedAcceptedFrontier.Len(), b.bootstrapAttempts)
	}

	b.Config.SharedCfg.RequestID++
	b.acceptedFrontier = b.acceptedFrontierSet.List()
	return b.sendGetAccepted()
}

// GetAcceptedFrontierFailed implements the AcceptedFrontierHandler interface.
func (b *bootstrapper) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("Received an Out-of-Sync GetAcceptedFrontierFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, b.Config.SharedCfg.RequestID, requestID)
		return nil
	}

	if err := b.gR.AddFailed(message.AcceptedFrontier, validatorID); err != nil {
		return err
	}

	return b.AcceptedFrontier(validatorID, requestID, nil)
}

// Accepted implements the AcceptedHandler interface.
func (b *bootstrapper) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	// ignores any late responses
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("Received an Out-of-Sync Accepted - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, b.Config.SharedCfg.RequestID, requestID)
		return nil
	}

	if !b.gR.ConsumeRequested(message.Accepted, validatorID) {
		return nil
	}

	weight := uint64(0)
	if w, ok := b.Beacons.GetWeight(validatorID); ok {
		weight = w
	}

	for _, containerID := range containerIDs {
		previousWeight := b.acceptedVotes[containerID]
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			b.Ctx.Log.Error("Error calculating the Accepted votes - weight: %v, previousWeight: %v", weight, previousWeight)
			newWeight = stdmath.MaxUint64
		}
		b.acceptedVotes[containerID] = newWeight
	}

	if err := b.sendGetAccepted(); err != nil {
		return err
	}

	// wait on pending responses
	if b.gR.CountRequested(message.Accepted) != 0 {
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
	failedAccepted := b.gR.GetAllFailed(message.Accepted)
	if size == 0 && b.Beacons.Len() > 0 {
		// retry the bootstrap if the weight is not enough to bootstrap
		failedBeaconWeight, err := b.Beacons.SubsetWeight(failedAccepted)
		if err != nil {
			return err
		}

		// in a zero network there will be no accepted votes but the voting weight will be greater than the failed weight
		if b.Config.RetryBootstrap && b.Beacons.Weight()-b.Alpha < failedBeaconWeight {
			b.Ctx.Log.Debug("Not enough votes received, restarting bootstrap... - Beacons: %d - Failed Bootstrappers: %d "+
				"- bootstrap attempt: %d", b.Beacons.Len(), failedAccepted.Len(), b.bootstrapAttempts)
			return b.Restart(false)
		}
	}

	if !b.Config.SharedCfg.Restarted {
		b.Ctx.Log.Info("Bootstrapping started syncing with %d vertices in the accepted frontier", size)
	} else {
		b.Ctx.Log.Debug("Bootstrapping started syncing with %d vertices in the accepted frontier", size)
	}

	return b.Bootstrapable.ForceAccepted(accepted)
}

// GetAcceptedFailed implements the AcceptedHandler interface.
func (b *bootstrapper) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != b.Config.SharedCfg.RequestID {
		b.Ctx.Log.Debug("Received an Out-of-Sync GetAcceptedFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID, b.Config.SharedCfg.RequestID, requestID)
		return nil
	}

	// If we can't get a response from [validatorID], act as though they said
	// that they think none of the containers we sent them in GetAccepted are accepted
	if err := b.gR.AddFailed(message.Accepted, validatorID); err != nil {
		return err
	}

	return b.Accepted(validatorID, requestID, nil)
}

func (b *bootstrapper) Startup() error {
	beacons, err := b.Beacons.Sample(b.Config.SampleK)
	if err != nil {
		return err
	}

	b.sampledBeacons = validators.NewSet()
	err = b.sampledBeacons.Set(beacons)
	if err != nil {
		return err
	}

	b.gR.ClearToRequest(message.AcceptedFrontier)
	for _, vdr := range beacons {
		vdrID := vdr.ID()
		if err := b.gR.PushToRequest(message.AcceptedFrontier, vdrID); err != nil {
			return err
		}
	}
	b.gR.ClearRequested(message.AcceptedFrontier)
	b.gR.ClearFailed(message.AcceptedFrontier)
	b.acceptedFrontierSet.Clear()

	b.gR.ClearToRequest(message.Accepted)
	for _, vdr := range b.Beacons.List() {
		vdrID := vdr.ID()
		if err := b.gR.PushToRequest(message.Accepted, vdrID); err != nil {
			return err
		}
	}

	b.gR.ClearRequested(message.Accepted)
	b.gR.ClearFailed(message.Accepted)
	b.acceptedVotes = make(map[ids.ID]uint64)

	b.bootstrapAttempts++
	if !b.gR.HasToRequest(message.AcceptedFrontier) {
		b.Ctx.Log.Info("Bootstrapping skipped due to no provided bootstraps")
		return b.Bootstrapable.ForceAccepted(nil)
	}

	b.Config.SharedCfg.RequestID++
	return b.sendGetAcceptedFrontiers()
}

func (b *bootstrapper) Restart(reset bool) error {
	// resets the attempts when we're pulling blocks/vertices we don't want to
	// fail the bootstrap at that stage
	if reset {
		b.Ctx.Log.Debug("Checking for new frontiers")

		b.Config.SharedCfg.Restarted = true
		b.bootstrapAttempts = 0
	}

	if b.bootstrapAttempts > 0 && b.bootstrapAttempts%b.RetryBootstrapWarnFrequency == 0 {
		b.Ctx.Log.Debug("continuing to attempt to bootstrap after %d failed attempts. Is this node connected to the internet?",
			b.bootstrapAttempts)
	}

	return b.Startup()
}

// Ask up to [MaxOutstandingBootstrapRequests] bootstrap validators to send
// their accepted frontier with the current accepted frontier
func (b *bootstrapper) sendGetAcceptedFrontiers() error {
	validators := ids.NewShortSet(1)

	frontiersToRequest := MaxOutstandingBootstrapRequests - b.gR.CountRequested(message.AcceptedFrontier)
	vdrsList := b.gR.PopToRequest(message.AcceptedFrontier, frontiersToRequest)
	if err := b.gR.RecordRequested(message.AcceptedFrontier, vdrsList); err != nil {
		return err
	}
	validators.Add(vdrsList...)

	if validators.Len() > 0 {
		b.Sender.SendGetAcceptedFrontier(validators, b.Config.SharedCfg.RequestID)
	}

	return nil
}

// Ask up to [MaxOutstandingBootstrapRequests] bootstrap validators to send
// their filtered accepted frontier
func (b *bootstrapper) sendGetAccepted() error {
	vdrs := ids.NewShortSet(1)

	acceptedFrontiersToRequest := MaxOutstandingBootstrapRequests - b.gR.CountRequested(message.Accepted)
	vdrsList := b.gR.PopToRequest(message.Accepted, acceptedFrontiersToRequest)
	if err := b.gR.RecordRequested(message.Accepted, vdrsList); err != nil {
		return err
	}
	vdrs.Add(vdrsList...)

	if vdrs.Len() > 0 {
		b.Ctx.Log.Debug("sent %d more GetAccepted messages with %d more to send",
			vdrs.Len(), b.gR.CountRequested(message.Accepted))
		b.Sender.SendGetAccepted(vdrs, b.Config.SharedCfg.RequestID, b.acceptedFrontier)
	}

	return nil
}
