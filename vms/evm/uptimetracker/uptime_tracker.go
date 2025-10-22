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

// UptimeTracker tracks uptime information for validators
type UptimeTracker struct {
	validatorState validators.State
	subnetID       ids.ID
	manager        uptime.Manager

	lock                sync.Mutex
	height              uint64
	state               *state
	synced              bool
	connectedValidators set.Set[ids.NodeID]
	// Deactivated validators are treated as being offline
	deactivatedValidators set.Set[ids.NodeID]
}

// New returns a new instance of UptimeTracker
func New(
	validatorState validators.State,
	subnetID ids.ID,
	db database.Database,
	clock *mockable.Clock,
) (*UptimeTracker, error) {
	s, err := newState(db)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize state: %w", err)
	}

	return &UptimeTracker{
		validatorState: validatorState,
		subnetID:       subnetID,
		manager:        uptime.NewManager(s, clock),
		synced:         false,
		state:          s,
	}, nil
}

// GetUptime returns the uptime of the validator corresponding to validationID
func (u *UptimeTracker) GetUptime(validationID ids.ID) (
	time.Duration,
	time.Time,
	bool,
	error,
) {
	u.lock.Lock()
	defer u.lock.Unlock()

	nodeID, ok := u.state.getNodeID(validationID)
	if !ok {
		return 0, time.Time{}, false, nil
	}

	uptime, lastUpdated, err := u.manager.CalculateUptime(nodeID)
	if err != nil {
		return 0, time.Time{}, false, fmt.Errorf(
			"failed to calculate uptime for validator %s: %w",
			validationID,
			err,
		)
	}

	return uptime, lastUpdated, true, nil
}

// Connect starts tracking a node. Nodes that are activated and connected will
// be treated as online.
func (u *UptimeTracker) Connect(nodeID ids.NodeID) error {
	u.lock.Lock()
	defer u.lock.Unlock()

	u.connectedValidators.Add(nodeID)
	if u.deactivatedValidators.Contains(nodeID) {
		return nil
	}

	return u.manager.Connect(nodeID)
}

// Disconnect stops tracking a node. Disconnected nodes are treated as being
// offline.
func (u *UptimeTracker) Disconnect(nodeID ids.NodeID) error {
	u.lock.Lock()
	defer u.lock.Unlock()

	u.connectedValidators.Remove(nodeID)
	if u.deactivatedValidators.Contains(nodeID) {
		return nil
	}

	return u.manager.Disconnect(nodeID)
}

// Sync updates the validator set and writes our state. Sync starts tracking
// uptimes for all active validators the first time it is called.
func (u *UptimeTracker) Sync(ctx context.Context) error {
	u.lock.Lock()
	defer u.lock.Unlock()

	currentValidatorSet, height, err := u.validatorState.GetCurrentValidatorSet(
		ctx,
		u.subnetID,
	)
	if err != nil {
		return fmt.Errorf("failed to get current validator set: %w", err)
	}

	// We are behind and need to update our local state
	if u.height < height {
		if err := u.update(height, currentValidatorSet); err != nil {
			return fmt.Errorf("failed to update validator set: %w", err)
		}
	}

	// Initialize uptimes if this is the first time Sync has been called
	if !u.synced {
		if err := u.manager.StartTracking(u.state.getNodeIDs()); err != nil {
			return fmt.Errorf("failed to start tracking validators: %w", err)
		}

		u.synced = true
	}

	if err := u.state.writeModifications(); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	return nil
}

func (u *UptimeTracker) update(
	height uint64,
	currentValidatorSet map[ids.ID]*validators.GetCurrentValidatorOutput,
) error {
	newValidators := currentValidatorSet

	for validationID, validator := range u.state.validators {
		// This validator is still in the latest update
		if _, ok := newValidators[validationID]; ok {
			continue
		}

		if !u.state.deleteValidator(validationID) {
			return fmt.Errorf("failed to delete validator %s", validationID)
		}

		u.deactivatedValidators.Remove(validator.NodeID)
	}

	// Add or update validators
	for validationID, newValidator := range newValidators {
		if ok := u.state.hasValidationID(validationID); ok {
			// Check if there was a status change
			if !u.state.updateValidator(validationID, newValidator.IsActive) {
				continue
			}

			// If there was a status change we need to activate/deactivate the
			// validator
			if newValidator.IsActive {
				// This validator is now active and is treated as online
				if err := u.activate(newValidator.NodeID); err != nil {
					return fmt.Errorf(
						"failed to activate validator %s: %w",
						newValidator.NodeID,
						err,
					)
				}
				continue
			}

			// This validator is no longer active and is treated as offline
			if err := u.deactivate(newValidator.NodeID); err != nil {
				return fmt.Errorf(
					"failed to deactivate validator %s: %w",
					newValidator.NodeID,
					err,
				)
			}
			continue
		}

		// This is a new validator
		u.state.addNewValidator(&validator{
			NodeID:        newValidator.NodeID,
			validationID:  validationID,
			IsActive:      newValidator.IsActive,
			StartTime:     newValidator.StartTime,
			UpDuration:    0,
			LastUpdated:   newValidator.StartTime,
			IsL1Validator: newValidator.IsL1Validator,
			Weight:        newValidator.Weight,
		})

		if newValidator.IsActive {
			continue
		}

		// This validator is not active and is treated is offline
		if err := u.deactivate(newValidator.NodeID); err != nil {
			return fmt.Errorf(
				"failed to deactivate validator %s: %w",
				newValidator.NodeID,
				err,
			)
		}
	}

	u.height = height
	return nil
}

// activate treats nodeID as online
func (u *UptimeTracker) activate(nodeID ids.NodeID) error {
	u.deactivatedValidators.Remove(nodeID)

	return u.manager.Connect(nodeID)
}

// deactivate treats nodeID as offline
func (u *UptimeTracker) deactivate(nodeID ids.NodeID) error {
	u.deactivatedValidators.Add(nodeID)

	return u.manager.Disconnect(nodeID)
}

// Shutdown stops tracking uptimes and writes our state.
func (u *UptimeTracker) Shutdown() error {
	u.lock.Lock()
	defer u.lock.Unlock()

	if !u.synced {
		return nil
	}

	if err := u.manager.StopTracking(u.state.getNodeIDs()); err != nil {
		return fmt.Errorf("failed to stop uptime tracking: %w", err)
	}

	return u.state.writeModifications()
}
