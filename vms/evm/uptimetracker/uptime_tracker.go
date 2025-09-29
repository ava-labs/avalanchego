// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

// Validator represents a validator in the state
type Validator struct {
	// ValidationID is the TxID of the tx on the P-Chain that added this
	// validator.
	ValidationID   ids.ID     `json:"validationID"`
	NodeID         ids.NodeID `json:"nodeID"`
	Weight         uint64     `json:"weight"`
	StartTimestamp uint64     `json:"startTimestamp"`
	IsActive       bool       `json:"isActive"`
	IsL1Validator  bool       `json:"isL1Validator"`
}

// UptimeTracker maintains local validator state synchronized with the P-Chain validator set.
// It tracks validator uptime and manages validator lifecycle events (additions, updates, removals)
// for the EVM subnet.
type UptimeTracker struct {
	validatorState validators.State
	subnetID       ids.ID
	manager    uptime.Manager
	clock *mockable.Clock

	state      *state
	connectedVdrs set.Set[ids.NodeID]
	// TODO godoc
	deactivatedValidators set.Set[ids.NodeID]
}

// NewUptimeTracker returns a new validator state
// that manages the validator state and the uptime manager.
// ValidatorState is not thread safe and should be used with the VM locked.
func NewUptimeTracker(
	validatorState validators.State,
	subnetID ids.ID,
	db database.Database,
	clock *mockable.Clock,
) (*UptimeTracker, error) {
	state, err := newState(db)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state: %w", err)
	}

	return &UptimeTracker{
		validatorState: validatorState,
		subnetID:       subnetID,
		manager:        uptime.NewManager(state, clock),
		state:          state,
		clock:          clock,
	}, nil
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

// GetValidationID returns the validation ID for the given nodeID
func (u *UptimeTracker) GetValidationID(nodeID ids.NodeID) (ids.ID, bool) {
	vID, ok := u.state.index[nodeID]
	if !ok {
		return ids.ID{}, false
	}

	return vID, true
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
// TODO to lock or not to lock?
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
// Paused validators are not connected until they activate operation.
func (u *UptimeTracker) Connect(nodeID ids.NodeID) error {
	u.connectedVdrs.Add(nodeID)
	if u.deactivatedValidators.Contains(nodeID) {
		return nil
	}

	return u.manager.Connect(nodeID)
}

func (u *UptimeTracker) Disconnect(nodeID ids.NodeID) error {
	u.connectedVdrs.Remove(nodeID)
	if u.deactivatedValidators.Contains(nodeID) {
		return nil
	}

	return u.manager.Disconnect(nodeID)
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
		if !u.state.deleteValidator(vID) {
			return fmt.Errorf("failed to delete validator %s", vID)
		}

		if !u.deactivatedValidators.Contains(validator.NodeID) {
			continue
		}

		if err := u.activate(validator.NodeID); err != nil {
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
			if err := u.state.updateValidator(validator); err != nil {
				return fmt.Errorf("failed to update validator %s: %w", vID, err)
			}
			if prev.IsActive == validator.IsActive {
				continue
			}

			if validator.IsActive {
				if err := u.activate(validator.NodeID); err != nil {
					return err
				}
			} else {
				if err := u.deactivate(validator.NodeID); err != nil {
					return err
				}
			}
		} else {
			if err := u.state.addValidator(validator); err != nil {
				return fmt.Errorf("failed to add validator %s: %w", vID, err)
			}
			if validator.IsActive {
				continue
			}

			err := u.deactivate(validator.NodeID)
			if err != nil {
				return err
			}
		}
	}

	// ValidatorState persists the state to disk at the end of every sync operation. The VM also
	// persists the validator database when the node is shutting down.
	return u.state.writeState()
}

// activate resumes uptime tracking for the node with the given ID
// activate can connect the node to the uptime.Manager if it was connected.
//
// When a paused validator peer resumes, meaning its status becomes `active`, the pausable uptime
// manager resumes the uptime tracking of the validator. It treats the peer as if it is connected
// to the tracker node.
func (u *UptimeTracker) activate(nodeID ids.NodeID) error {
	u.deactivatedValidators.Remove(nodeID)

	return u.manager.Connect(nodeID)
}

// deactivate pauses uptime tracking for the node with the given ID
// deactivate can disconnect the node from the uptime.Manager if it is connected.
//
// The pausable uptime manager can listen for validator status changes by subscribing to the state.
// When the state invokes the `OnValidatorStatusChange` method, the pausable uptime manager pauses
// the uptime tracking of the validator if the validator is currently `inactive`. When a validator
// is paused, it is treated as if it is disconnected from the tracker node; thus, its uptime is
// updated from the connection time to the deactivate time, and uptime manager stops tracking the
// uptime of the validator.
func (u *UptimeTracker) deactivate(nodeID ids.NodeID) error {
	u.deactivatedValidators.Add(nodeID)

	return u.manager.Disconnect(nodeID)
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

	return u.state.writeState()
}
