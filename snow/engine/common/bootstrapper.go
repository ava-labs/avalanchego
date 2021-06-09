// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"fmt"
	"time"

	stdmath "math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/math"
)

const (
	// MaxContainersPerMultiPut is the maximum number of containers that can be
	// sent in a MultiPut message
	MaxContainersPerMultiPut = 2000

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

// Bootstrapper implements the Engine interface.
type Bootstrapper struct {
	Config
	Halter

	// Tracks the last requestID that was used in a request
	RequestID uint32

	// True if RestartBootstrap has been called at least once
	Restarted bool

	// Holds the beacons that were sampled for the accepted frontier
	sampledBeacons validators.Set
	// IDs of validators we should request an accepted frontier from
	pendingSendAcceptedFrontier ids.ShortSet
	// IDs of validators we requested an accepted frontier from but haven't
	// received a reply yet
	pendingReceiveAcceptedFrontier ids.ShortSet
	// IDs of validators that failed to respond with their accepted frontier
	failedAcceptedFrontier ids.ShortSet
	// IDs of all the returned accepted frontiers
	acceptedFrontierSet ids.Set

	// IDs of validators we should request filtering the accepted frontier from
	pendingSendAccepted ids.ShortSet
	// IDs of validators we requested filtering the accepted frontier from but
	// haven't received a reply yet
	pendingReceiveAccepted ids.ShortSet
	// IDs of validators that failed to respond with their filtered accepted
	// frontier
	failedAccepted ids.ShortSet
	// IDs of the returned accepted containers and the stake weight that has
	// marked them as accepted
	acceptedVotes    map[ids.ID]uint64
	acceptedFrontier []ids.ID

	// current weight
	started bool
	weight  uint64

	// number of times the bootstrap has been attempted
	bootstrapAttempts int
}

// Initialize implements the Engine interface.
func (b *Bootstrapper) Initialize(config Config) error {
	b.Config = config
	b.Ctx.Log.Info("Starting bootstrap...")

	if b.Config.StartupAlpha > 0 {
		return nil
	}
	return b.startup()
}

// GetAcceptedFrontier implements the Engine interface.
func (b *Bootstrapper) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) error {
	acceptedFrontier, err := b.Bootstrapable.CurrentAcceptedFrontier()
	if err != nil {
		return err
	}
	b.Sender.AcceptedFrontier(validatorID, requestID, acceptedFrontier)
	return nil
}

// GetAcceptedFrontierFailed implements the Engine interface.
func (b *Bootstrapper) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != b.RequestID {
		b.Ctx.Log.Debug("Received an Out-of-Sync GetAcceptedFrontierFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID,
			b.RequestID,
			requestID)
		return nil
	}

	// If we can't get a response from [validatorID], act as though they said their accepted frontier is empty
	// and we add the validator to the failed list
	b.failedAcceptedFrontier.Add(validatorID)
	return b.AcceptedFrontier(validatorID, requestID, nil)
}

// AcceptedFrontier implements the Engine interface.
func (b *Bootstrapper) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	// ignores any late responses
	if requestID != b.RequestID {
		b.Ctx.Log.Debug("Received an Out-of-Sync AcceptedFrontier - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID,
			b.RequestID,
			requestID)
		return nil
	}

	if !b.pendingReceiveAcceptedFrontier.Contains(validatorID) {
		b.Ctx.Log.Debug("Received an AcceptedFrontier message from %s unexpectedly", validatorID)
		return nil
	}

	// Mark that we received a response from [validatorID]
	b.pendingReceiveAcceptedFrontier.Remove(validatorID)

	// Union the reported accepted frontier from [validatorID] with the accepted frontier we got from others
	b.acceptedFrontierSet.Add(containerIDs...)

	b.sendGetAcceptedFrontiers()

	// still waiting on requests
	if b.pendingReceiveAcceptedFrontier.Len() != 0 {
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

	failedBeaconWeight, err := b.Beacons.SubsetWeight(b.failedAcceptedFrontier)
	if err != nil {
		return err
	}

	// fail the bootstrap if the weight is not enough to bootstrap
	if float64(b.sampledBeacons.Weight())-newAlpha < float64(failedBeaconWeight) {
		if b.Config.RetryBootstrap {
			b.Ctx.Log.Debug("Not enough frontiers received, restarting bootstrap... - Beacons: %d - Failed Bootstrappers: %d "+
				"- bootstrap attempt: %d", b.Beacons.Len(), b.failedAcceptedFrontier.Len(), b.bootstrapAttempts)
			return b.RestartBootstrap(false)
		}

		b.Ctx.Log.Debug("Didn't receive enough frontiers - failed validators: %d, "+
			"bootstrap attempt: %d", b.failedAcceptedFrontier.Len(), b.bootstrapAttempts)
	}

	b.RequestID++
	b.acceptedFrontier = b.acceptedFrontierSet.List()

	b.sendGetAccepted()
	return nil
}

// GetAccepted implements the Engine interface.
func (b *Bootstrapper) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	b.Sender.Accepted(validatorID, requestID, b.Bootstrapable.FilterAccepted(containerIDs))
	return nil
}

// GetAcceptedFailed implements the Engine interface.
func (b *Bootstrapper) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	// ignores any late responses
	if requestID != b.RequestID {
		b.Ctx.Log.Debug("Received an Out-of-Sync GetAcceptedFailed - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID,
			b.RequestID,
			requestID)
		return nil
	}

	// If we can't get a response from [validatorID], act as though they said
	// that they think none of the containers we sent them in GetAccepted are accepted
	b.failedAccepted.Add(validatorID)
	return b.Accepted(validatorID, requestID, nil)
}

// Accepted implements the Engine interface.
func (b *Bootstrapper) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	// ignores any late responses
	if requestID != b.RequestID {
		b.Ctx.Log.Debug("Received an Out-of-Sync Accepted - validator: %v - expectedRequestID: %v, requestID: %v",
			validatorID,
			b.RequestID,
			requestID)
		return nil
	}

	if !b.pendingReceiveAccepted.Contains(validatorID) {
		b.Ctx.Log.Debug("Received an Accepted message from %s unexpectedly", validatorID)
		return nil
	}
	// Mark that we received a response from [validatorID]
	b.pendingReceiveAccepted.Remove(validatorID)

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

	b.sendGetAccepted()

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
		failedBeaconWeight, err := b.Beacons.SubsetWeight(b.failedAccepted)
		if err != nil {
			return err
		}

		// in a zero network there will be no accepted votes but the voting weight will be greater than the failed weight
		if b.Config.RetryBootstrap && b.Beacons.Weight()-b.Alpha < failedBeaconWeight {
			b.Ctx.Log.Debug("Not enough votes received, restarting bootstrap... - Beacons: %d - Failed Bootstrappers: %d "+
				"- bootstrap attempt: %d", b.Beacons.Len(), b.failedAccepted.Len(), b.bootstrapAttempts)
			return b.RestartBootstrap(false)
		}
	}

	if !b.Restarted {
		b.Ctx.Log.Info("Bootstrapping started syncing with %d vertices in the accepted frontier", size)
	} else {
		b.Ctx.Log.Debug("Bootstrapping started syncing with %d vertices in the accepted frontier", size)
	}

	return b.Bootstrapable.ForceAccepted(accepted)
}

// Connected implements the Engine interface.
func (b *Bootstrapper) Connected(validatorID ids.ShortID) error {
	if b.started {
		return nil
	}
	weight, ok := b.Beacons.GetWeight(validatorID)
	if !ok {
		return nil
	}
	weight, err := math.Add64(weight, b.weight)
	if err != nil {
		return err
	}
	b.weight = weight
	if b.weight < b.StartupAlpha {
		return nil
	}
	return b.startup()
}

// Disconnected implements the Engine interface.
func (b *Bootstrapper) Disconnected(validatorID ids.ShortID) error {
	if weight, ok := b.Beacons.GetWeight(validatorID); ok {
		// TODO: Account for weight changes in a more robust manner.

		// Sub64 should rarely error since only validators that have added their
		// weight can become disconnected. Because it is possible that there are
		// changes to the validators set, we utilize that Sub64 returns 0 on
		// error.
		b.weight, _ = math.Sub64(b.weight, weight)
	}
	return nil
}

func (b *Bootstrapper) RestartBootstrap(reset bool) error {
	// resets the attempts when we're pulling blocks/vertices we don't want to
	// fail the bootstrap at that stage
	if reset {
		b.Ctx.Log.Debug("Checking for new frontiers")

		b.Restarted = true
		b.bootstrapAttempts = 0
	}

	if b.bootstrapAttempts >= b.RetryBootstrapMaxAttempts {
		return fmt.Errorf("failed to bootstrap the chain after %d attempts", b.bootstrapAttempts)
	}

	return b.startup()
}

func (b *Bootstrapper) startup() error {
	b.started = true

	beacons, err := b.Beacons.Sample(b.Config.SampleK)
	if err != nil {
		return err
	}

	b.sampledBeacons = validators.NewSet()
	err = b.sampledBeacons.Set(beacons)
	if err != nil {
		return err
	}

	b.pendingSendAcceptedFrontier.Clear()
	for _, vdr := range beacons {
		vdrID := vdr.ID()
		b.pendingSendAcceptedFrontier.Add(vdrID)
	}

	b.pendingReceiveAcceptedFrontier.Clear()
	b.failedAcceptedFrontier.Clear()
	b.acceptedFrontierSet.Clear()

	b.pendingSendAccepted.Clear()
	for _, vdr := range b.Beacons.List() {
		vdrID := vdr.ID()
		b.pendingSendAccepted.Add(vdrID)
	}

	b.pendingReceiveAccepted.Clear()
	b.failedAccepted.Clear()
	b.acceptedVotes = make(map[ids.ID]uint64)

	b.bootstrapAttempts++
	if b.pendingSendAcceptedFrontier.Len() == 0 {
		b.Ctx.Log.Info("Bootstrapping skipped due to no provided bootstraps")
		return b.Bootstrapable.ForceAccepted(nil)
	}

	b.RequestID++
	b.sendGetAcceptedFrontiers()
	return nil
}

// Ask up to [MaxOutstandingBootstrapRequests] bootstrap validators to send
// their accepted frontier with the current accepted frontier
func (b *Bootstrapper) sendGetAcceptedFrontiers() {
	vdrs := ids.NewShortSet(MaxOutstandingBootstrapRequests)
	for b.pendingSendAcceptedFrontier.Len() > 0 && b.pendingReceiveAcceptedFrontier.Len() < MaxOutstandingBootstrapRequests {
		vdr, _ := b.pendingSendAcceptedFrontier.Pop()
		// Add the validator to the set to send the messages to
		vdrs.Add(vdr)
		// Add the validator to send pending receipt set
		b.pendingReceiveAcceptedFrontier.Add(vdr)
	}

	if vdrs.Len() > 0 {
		b.Sender.GetAcceptedFrontier(vdrs, b.RequestID)
	}
}

// Ask up to [MaxOutstandingBootstrapRequests] bootstrap validators to send
// their filtered accepted frontier
func (b *Bootstrapper) sendGetAccepted() {
	vdrs := ids.NewShortSet(MaxOutstandingBootstrapRequests)
	for b.pendingSendAccepted.Len() > 0 && b.pendingReceiveAccepted.Len() < MaxOutstandingBootstrapRequests {
		vdr, _ := b.pendingSendAccepted.Pop()
		// Add the validator to the set to send the messages to
		vdrs.Add(vdr)
		// Add the validator to send pending receipt set
		b.pendingReceiveAccepted.Add(vdr)
	}

	if vdrs.Len() > 0 {
		b.Ctx.Log.Debug("sent %d more GetAccepted messages with %d more to send",
			vdrs.Len(),
			b.pendingSendAccepted.Len(),
		)
		b.Sender.GetAccepted(vdrs, b.RequestID, b.acceptedFrontier)
	}
}
