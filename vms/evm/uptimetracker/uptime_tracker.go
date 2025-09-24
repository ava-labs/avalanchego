// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// UptimeTracker maintains local validator state synchronized with the P-Chain validator set.
// It tracks validator uptime and manages validator lifecycle events (additions, updates, removals)
// for the EVM subnet.
// TODO: consider adding a metrics object to the UptimeTracker
type UptimeTracker struct {
	validatorState validators.State
	subnetID       ids.ID
	// updatedData tracks the updates since WriteValidator was last called
	manager    uptime.Manager
	pausedVdrs set.Set[ids.NodeID]
	state      *state
	// connectedVdrs is a set of nodes that are connected to the manager.
	// This is used to immediately connect nodes when they are unpaused.
	connectedVdrs set.Set[ids.NodeID]
	clock         *mockable.Clock
}

// NewUptimeTracker returns a new validator state
// that manages the validator state and the uptime manager.
// ValidatorState is not thread safe and should be used with the VM locked.
func NewUptimeTracker(
	validatorState validators.State,
	subnetID ids.ID,
	db database.Database,
) (*UptimeTracker, error) {
	state, err := newState(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create state: %w", err)
	}
	clock := &mockable.Clock{}
	clock.Set(time.Now()) // Initialize with current time

	// Create the UptimeTracker first
	return &UptimeTracker{
		validatorState: validatorState,
		subnetID:       subnetID,
		manager:        uptime.NewManager(state, clock),
		pausedVdrs:     make(set.Set[ids.NodeID]),
		state:          state,
		connectedVdrs:  make(set.Set[ids.NodeID]),
		clock:          clock,
	}, nil
}

// Validator represents a validator in the state
type Validator struct {
	ValidationID   ids.ID     `json:"validationID"`   // Unique validation identifier
	NodeID         ids.NodeID `json:"nodeID"`         // Node identifier
	Weight         uint64     `json:"weight"`         // Validator weight/stake
	StartTimestamp uint64     `json:"startTimestamp"` // When validation started
	IsActive       bool       `json:"isActive"`       // Whether validator is currently active
	IsL1Validator  bool       `json:"isL1Validator"`  // Whether this is an L1 validator
}

// addValidator adds a new validator to the state
// the new validator is marked as updated and will be written to the disk when WriteState is called
func (u *UptimeTracker) addValidator(vdr Validator) error {
	data := &validatorData{
		NodeID:        vdr.NodeID,
		validationID:  vdr.ValidationID,
		IsActive:      vdr.IsActive,
		StartTime:     vdr.StartTimestamp,
		UpDuration:    0,
		LastUpdated:   vdr.StartTimestamp,
		IsL1Validator: vdr.IsL1Validator,
		Weight:        vdr.Weight,
	}
	if err := u.state.addData(vdr.ValidationID, data); err != nil {
		return err
	}

	u.state.updatedData[vdr.ValidationID] = updatedStatus
	return nil
}

// updateValidator updates the validator in the state
// returns an error if the validator does not exist or if the immutable fields are modified
func (u *UptimeTracker) updateValidator(vdr Validator) error {
	data, ok := u.state.data[vdr.ValidationID]
	if !ok {
		return database.ErrNotFound
	}
	// check immutable fields
	if !data.constantsAreUnmodified(vdr) {
		return ErrImmutableField
	}
	// check if mutable fields have changed
	updated := deletedStatus
	if data.IsActive != vdr.IsActive {
		data.IsActive = vdr.IsActive
		updated = updatedStatus
	}

	if data.Weight != vdr.Weight {
		data.Weight = vdr.Weight
		updated = updatedStatus
	}

	u.state.updatedData[vdr.ValidationID] = updated
	return nil
}

// deleteValidator marks the validator as deleted
// marked validator will be deleted from disk when WriteState is called
func (u *UptimeTracker) deleteValidator(vID ids.ID) bool {
	data, ok := u.state.data[vID]
	if !ok {
		return false
	}
	delete(u.state.data, data.validationID)
	delete(u.state.index, data.NodeID)

	// mark as deleted for WriteValidator
	u.state.updatedData[data.validationID] = deletedStatus
	return true
}

// GetValidator returns the validator data for the given validationID
func (u *UptimeTracker) GetValidator(vID ids.ID) (Validator, bool) {
	data, ok := u.state.data[vID]
	if !ok {
		return Validator{}, false
	}
	return Validator{
		ValidationID:   data.validationID,
		NodeID:         data.NodeID,
		StartTimestamp: data.StartTime,
		IsActive:       data.IsActive,
		Weight:         data.Weight,
		IsL1Validator:  data.IsL1Validator,
	}, true
}

// GetValidators returns all validators in the state.
func (u *UptimeTracker) GetValidators() []Validator {
	validators := make([]Validator, 0, len(u.state.data))
	for _, vdr := range u.state.data {
		validators = append(validators, Validator{
			ValidationID:   vdr.validationID,
			NodeID:         vdr.NodeID,
			Weight:         vdr.Weight,
			StartTimestamp: vdr.StartTime,
			IsActive:       vdr.IsActive,
			IsL1Validator:  vdr.IsL1Validator,
		})
	}
	return validators
}

// GetValidatorAndUptime returns the calculated uptime of the validator specified by [validationID]
// and the last updated time.
// GetValidatorAndUptime holds the lock while performing the operation and can be called concurrently.
func (u *UptimeTracker) GetValidatorAndUptime(validationID ids.ID, lock sync.Locker) (Validator, time.Duration, time.Time, error) {
	lock.Lock()
	defer lock.Unlock()

	vdr, ok := u.GetValidator(validationID)
	if !ok {
		return Validator{}, 0, time.Time{}, fmt.Errorf("failed to get validator %s", validationID)
	}

	uptime, lastUpdated, err := u.manager.CalculateUptime(vdr.NodeID)
	if err != nil {
		return Validator{}, 0, time.Time{}, fmt.Errorf("failed to calculate uptime for validator %s: %w", validationID, err)
	}

	return vdr, uptime, lastUpdated, nil
}

// Connect connects a node to the uptime manager for tracking.
// Paused validators are not connected until they resume operation.
func (u *UptimeTracker) Connect(nodeID ids.NodeID) error {
	u.connectedVdrs.Add(nodeID)
	if !u.isPaused(nodeID) && !u.manager.IsConnected(nodeID) {
		return u.manager.Connect(nodeID)
	}
	return nil
}

// IsConnected returns true if the node with the given ID is connected to the uptime tracker.
func (u *UptimeTracker) IsConnected(nodeID ids.NodeID) bool {
	return u.connectedVdrs.Contains(nodeID)
}

// Disconnect disconnects the node with the given ID from the uptime.Manager
// If the node is paused, it will not be disconnected
// Invariant: we should never have a connected paused node that is disconnecting
//
// When a peer validator is disconnected, the AvalancheGo uptime manager updates the uptime of the
// validator by adding the duration between the connection time and the disconnection time to the
// uptime of the validator. When a validator is paused/`inactive`, the pausable uptime manager
// handles the `inactive` peers as if they were disconnected. Thus the uptime manager assumes that
// no paused peers can be disconnected again from the pausable uptime manager.
func (u *UptimeTracker) Disconnect(nodeID ids.NodeID) error {
	u.connectedVdrs.Remove(nodeID)
	if u.manager.IsConnected(nodeID) {
		return u.manager.Disconnect(nodeID)
	}
	return nil
}

// resume resumes uptime tracking for the node with the given ID
// resume can connect the node to the uptime.Manager if it was connected.
//
// When a paused validator peer resumes, meaning its status becomes `active`, the pausable uptime
// manager resumes the uptime tracking of the validator. It treats the peer as if it is connected
// to the tracker node.
func (u *UptimeTracker) resume(nodeID ids.NodeID) error {
	u.pausedVdrs.Remove(nodeID)
	if u.connectedVdrs.Contains(nodeID) && !u.manager.IsConnected(nodeID) {
		return u.manager.Connect(nodeID)
	}
	return nil
}

// pause pauses uptime tracking for the node with the given ID
// pause can disconnect the node from the uptime.Manager if it is connected.
//
// The pausable uptime manager can listen for validator status changes by subscribing to the state.
// When the state invokes the `OnValidatorStatusChange` method, the pausable uptime manager pauses
// the uptime tracking of the validator if the validator is currently `inactive`. When a validator
// is paused, it is treated as if it is disconnected from the tracker node; thus, its uptime is
// updated from the connection time to the pause time, and uptime manager stops tracking the
// uptime of the validator.
func (u *UptimeTracker) pause(nodeID ids.NodeID) error {
	u.pausedVdrs.Add(nodeID)
	if u.manager.IsConnected(nodeID) {
		// If the node is connected, then we need to disconnect it from
		// manager
		// This should be fine in case tracking has not started yet since
		// the inner manager should handle disconnects accordingly
		return u.manager.Disconnect(nodeID)
	}
	return nil
}

// isPaused returns true if the node with the given ID is paused.
func (u *UptimeTracker) isPaused(nodeID ids.NodeID) bool {
	return u.pausedVdrs.Contains(nodeID)
}

// Shutdown stops uptime tracking and persists the validator state
func (u *UptimeTracker) Shutdown() error {
	validators := u.GetValidators()
	vdrIDs := make([]ids.NodeID, 0, len(validators))
	for _, vdr := range validators {
		vdrIDs = append(vdrIDs, vdr.NodeID)
	}
	if err := u.manager.StopTracking(vdrIDs); err != nil {
		return fmt.Errorf("failed to stop uptime tracking: %w", err)
	}
	if !u.state.writeState() {
		return errors.New("failed to write validator state")
	}

	return nil
}

// Sync synchronizes the validator state with the current validator set and writes the state to the database.
// Sync is not safe to call concurrently and should be called with the VM locked.
func (u *UptimeTracker) Sync(ctx context.Context) error {
	// Get current validator set from P-Chain. P-Chain's `GetCurrentValidatorSet` can report both
	// L1 and Subnet validators. Subnet-EVM's uptime manager also tracks both of these validator
	// types. So even if a the Subnet has not yet been converted to an L1, the uptime and validator
	// state tracking is still performed by Subnet-EVM.
	currentValidatorSet, _, err := u.validatorState.GetCurrentValidatorSet(ctx, u.subnetID)
	if err != nil {
		return fmt.Errorf("failed to get current validator set: %w", err)
	}

	currentValidators := u.GetValidators()
	currentValidationIDs := set.NewSet[ids.ID](len(currentValidators))
	for _, vdr := range currentValidators {
		currentValidationIDs.Add(vdr.ValidationID)
	}
	newValidators := currentValidatorSet

	// Remove validators no longer in the current set
	for vID := range currentValidationIDs {
		if _, ok := newValidators[vID]; ok {
			continue
		}

		// fetch validator for nodeID prior to deletion
		validator, ok := u.GetValidator(vID)
		if !ok {
			return fmt.Errorf("failed to fetch validator %s", vID)
		}
		if !u.deleteValidator(vID) {
			return fmt.Errorf("failed to delete validator %s", vID)
		}

		if !u.isPaused(validator.NodeID) {
			continue
		}

		if err := u.resume(validator.NodeID); err != nil {
			return err
		}
	}

	// Add or update validators
	for vID, newVdr := range newValidators {
		validator := Validator{
			ValidationID:   vID,
			NodeID:         newVdr.NodeID,
			Weight:         newVdr.Weight,
			StartTimestamp: newVdr.StartTime,
			IsActive:       newVdr.IsActive,
			IsL1Validator:  newVdr.IsL1Validator,
		}

		if currentValidationIDs.Contains(vID) {
			prev, ok := u.GetValidator(vID)
			if !ok {
				return fmt.Errorf("failed to update validator %s: missing previous state", vID)
			}
			if err := u.updateValidator(validator); err != nil {
				return fmt.Errorf("failed to update validator %s: %w", vID, err)
			}
			if prev.IsActive != validator.IsActive {
				var err error
				if validator.IsActive {
					err = u.resume(validator.NodeID)
				} else {
					err = u.pause(validator.NodeID)
				}
				if err != nil {
					log.Error("failed to update status for node %s: %v", validator.NodeID, err)
				}
			}
		} else {
			if err := u.addValidator(validator); err != nil {
				return fmt.Errorf("failed to add validator %s: %w", vID, err)
			}
			if !validator.IsActive {
				err := u.pause(validator.NodeID)
				if err != nil {
					log.Error("failed to handle added validator %s: %v", validator.NodeID, err)
				}
			}
		}
	}

	// ValidatorState persists the state to disk at the end of every sync operation. The VM also
	// persists the validator database when the node is shutting down.
	if !u.state.writeState() {
		return errors.New("failed to write validator state")
	}

	return nil
}

// GetValidationID returns the validation ID for the given nodeID
func (u *UptimeTracker) GetValidationID(nodeID ids.NodeID) (ids.ID, bool) {
	vID, ok := u.state.index[nodeID]
	if !ok {
		return ids.ID{}, false
	}
	return vID, true
}
