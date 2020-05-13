// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	stdmath "math"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/math"
)

// Bootstrapper implements the Engine interface.
type Bootstrapper struct {
	Config

	pendingAcceptedFrontier ids.ShortSet
	acceptedFrontier        ids.Set

	pendingAccepted ids.ShortSet
	acceptedVotes   map[[32]byte]uint64

	RequestID uint32
}

// Initialize implements the Engine interface.
func (b *Bootstrapper) Initialize(config Config) {
	b.Config = config

	for _, vdr := range b.Beacons.List() {
		vdrID := vdr.ID()
		b.pendingAcceptedFrontier.Add(vdrID)
		b.pendingAccepted.Add(vdrID)
	}

	b.acceptedVotes = make(map[[32]byte]uint64)
}

// Startup implements the Engine interface.
func (b *Bootstrapper) Startup() {
	if b.pendingAcceptedFrontier.Len() == 0 {
		b.Context.Log.Info("Bootstrapping skipped due to no provided bootstraps")
		b.Bootstrapable.ForceAccepted(ids.Set{})
		return
	}

	vdrs := ids.ShortSet{}
	vdrs.Union(b.pendingAcceptedFrontier)

	b.RequestID++
	b.Sender.GetAcceptedFrontier(vdrs, b.RequestID)
}

// GetAcceptedFrontier implements the Engine interface.
func (b *Bootstrapper) GetAcceptedFrontier(validatorID ids.ShortID, requestID uint32) {
	b.Sender.AcceptedFrontier(validatorID, requestID, b.Bootstrapable.CurrentAcceptedFrontier())
}

// GetAcceptedFrontierFailed implements the Engine interface.
func (b *Bootstrapper) GetAcceptedFrontierFailed(validatorID ids.ShortID, requestID uint32) {
	b.AcceptedFrontier(validatorID, requestID, ids.Set{})
}

// AcceptedFrontier implements the Engine interface.
func (b *Bootstrapper) AcceptedFrontier(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	if !b.pendingAcceptedFrontier.Contains(validatorID) {
		b.Context.Log.Debug("Received an AcceptedFrontier message from %s unexpectedly", validatorID)
		return
	}
	b.pendingAcceptedFrontier.Remove(validatorID)

	b.acceptedFrontier.Union(containerIDs)

	if b.pendingAcceptedFrontier.Len() == 0 {
		vdrs := ids.ShortSet{}
		vdrs.Union(b.pendingAccepted)

		b.RequestID++
		b.Sender.GetAccepted(vdrs, b.RequestID, b.acceptedFrontier)
	}
}

// GetAccepted implements the Engine interface.
func (b *Bootstrapper) GetAccepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	b.Sender.Accepted(validatorID, requestID, b.Bootstrapable.FilterAccepted(containerIDs))
}

// GetAcceptedFailed implements the Engine interface.
func (b *Bootstrapper) GetAcceptedFailed(validatorID ids.ShortID, requestID uint32) {
	b.Accepted(validatorID, requestID, ids.Set{})
}

// Accepted implements the Engine interface.
func (b *Bootstrapper) Accepted(validatorID ids.ShortID, requestID uint32, containerIDs ids.Set) {
	if !b.pendingAccepted.Contains(validatorID) {
		b.Context.Log.Debug("Received an Accepted message from %s unexpectedly", validatorID)
		return
	}
	b.pendingAccepted.Remove(validatorID)

	weight := uint64(0)
	if vdr, ok := b.Validators.Get(validatorID); ok {
		weight = vdr.Weight()
	}

	for _, containerID := range containerIDs.List() {
		key := containerID.Key()
		previousWeight := b.acceptedVotes[key]
		newWeight, err := math.Add64(weight, previousWeight)
		if err != nil {
			newWeight = stdmath.MaxUint64
		}
		b.acceptedVotes[key] = newWeight
	}

	if b.pendingAccepted.Len() == 0 {
		accepted := ids.Set{}
		for key, weight := range b.acceptedVotes {
			if weight >= b.Config.Alpha {
				accepted.Add(ids.NewID(key))
			}
		}

		if size := accepted.Len(); size == 0 && b.Config.Beacons.Len() > 0 {
			b.Context.Log.Warn("Bootstrapping finished with no accepted frontier. This is likely a result of failing to be able to connect to the specified bootstraps, or no transactions have been issued on this chain yet")
		} else {
			b.Context.Log.Info("Bootstrapping started syncing with %d vertices in the accepted frontier", size)
		}

		b.Bootstrapable.ForceAccepted(accepted)
	}
}
