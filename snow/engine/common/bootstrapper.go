// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"time"

	stdmath "math"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/math"
)

const (
	// MaxContainersPerMultiPut is the maximum number of containers that can be
	// sent in a MultiPut message
	MaxContainersPerMultiPut = 2000

	// StatusUpdateFrequency is how many containers should be processed between
	// logs
	StatusUpdateFrequency = 2500

	// MaxOutstandingRequests is the maximum number of GetAncestors sent but not
	// responded to/failed
	MaxOutstandingRequests = 8

	// MaxTimeFetchingAncestors is the maximum amount of time to spend fetching
	// vertices during a call to GetAncestors
	MaxTimeFetchingAncestors = 50 * time.Millisecond
)

// Bootstrapper implements the Engine interface.
type Bootstrapper struct {
	Config

	RequestID uint32

	// IDs of validators we have requested the accepted frontier from but haven't
	// received a reply from
	pendingAcceptedFrontier ids.ShortSet
	acceptedFrontier        ids.Set

	pendingAccepted ids.ShortSet
	acceptedVotes   map[ids.ID]uint64

	// current weight
	started bool
	weight  uint64
}

// Initialize implements the Engine interface.
func (b *Bootstrapper) Initialize(config Config) error {
	b.Config = config

	beacons, err := b.Beacons.Sample(config.SampleK)
	if err != nil {
		return err
	}

	for _, vdr := range beacons {
		vdrID := vdr.ID()
		b.pendingAcceptedFrontier.Add(vdrID)
	}

	for _, vdr := range b.Beacons.List() {
		vdrID := vdr.ID()
		b.pendingAccepted.Add(vdrID)
	}

	b.acceptedVotes = make(map[ids.ID]uint64)
	if b.Config.StartupAlpha > 0 {
		return nil
	}
	return b.Startup()
}

// Startup implements the Engine interface.
func (b *Bootstrapper) Startup() error {
	b.started = true
	if b.pendingAcceptedFrontier.Len() == 0 {
		b.Ctx.Log.Info("Bootstrapping skipped due to no provided bootstraps")
		return b.Bootstrapable.ForceAccepted(nil)
	}

	// Ask each of the bootstrap validators to send their accepted frontier
	vdrs := ids.ShortSet{}
	vdrs.Union(b.pendingAcceptedFrontier)

	b.RequestID++
	b.Sender.GetAcceptedFrontier(vdrs, b.RequestID)
	return nil
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
	// If we can't get a response from [validatorID], act as though they said their accepted frontier is empty
	return b.AcceptedFrontier(validatorID, requestID, nil)
}

// AcceptedFrontier implements the Engine interface.
func (b *Bootstrapper) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	if !b.pendingAcceptedFrontier.Contains(validatorID) {
		b.Ctx.Log.Debug("Received an AcceptedFrontier message from %s unexpectedly", validatorID)
		return nil
	}
	// Mark that we received a response from [validatorID]
	b.pendingAcceptedFrontier.Remove(validatorID)

	// Union the reported accepted frontier from [validatorID] with the accepted frontier we got from others
	b.acceptedFrontier.Add(containerIDs...)

	// We've received the accepted frontier from every bootstrap validator
	// Ask each bootstrap validator to filter the list of containers that we were
	// told are on the accepted frontier such that the list only contains containers
	// they think are accepted
	if b.pendingAcceptedFrontier.Len() == 0 {
		vdrs := ids.ShortSet{}
		vdrs.Union(b.pendingAccepted)

		b.RequestID++
		b.Sender.GetAccepted(vdrs, b.RequestID, b.acceptedFrontier.List())
	}
	return nil
}

// GetAccepted implements the Engine interface.
func (b *Bootstrapper) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	b.Sender.Accepted(validatorID, requestID, b.Bootstrapable.FilterAccepted(containerIDs))
	return nil
}

// GetAcceptedFailed implements the Engine interface.
func (b *Bootstrapper) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) error {
	// If we can't get a response from [validatorID], act as though they said
	// that they think none of the containers we sent them in GetAccepted are accepted
	return b.Accepted(validatorID, requestID, nil)
}

// Accepted implements the Engine interface.
func (b *Bootstrapper) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs []ids.ID) error {
	if !b.pendingAccepted.Contains(validatorID) {
		b.Ctx.Log.Debug("Received an Accepted message from %s unexpectedly", validatorID)
		return nil
	}
	// Mark that we received a response from [validatorID]
	b.pendingAccepted.Remove(validatorID)

	weight := uint64(0)
	if w, ok := b.Beacons.GetWeight(validatorID); ok {
		weight = w
	}

	for _, containerID := range containerIDs {
		previousWeight := b.acceptedVotes[containerID]
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			newWeight = stdmath.MaxUint64
		}
		b.acceptedVotes[containerID] = newWeight
	}

	if b.pendingAccepted.Len() != 0 {
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

	if size := len(accepted); size == 0 && b.Beacons.Len() > 0 {
		b.Ctx.Log.Info("Bootstrapping finished with no accepted frontier. This is likely a result of failing to be able to connect to the specified bootstraps, or no transactions have been issued on this chain yet")
	} else {
		b.Ctx.Log.Info("Bootstrapping started syncing with %d vertices in the accepted frontier", size)
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
	return b.Startup()
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
