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
	"errors"
)

var errNotFound = errors.New("not found")

// UptimeTracker tracks uptime information for validators
type UptimeTracker struct {
	validatorState validators.State
	subnetID       ids.ID
	manager    uptime.Manager
	clock *mockable.Clock

	lock sync.Mutex
	height int
	state      *state
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
		manager: uptime.NewManager(s, clock),
		clock:  clock,
		height: -1,
		state:   s,
	}, nil
}

// GetUptime returns the uptime of the validator corresponding to validationID
func (u *UptimeTracker) GetUptime(validationID ids.ID) (
	time.Duration,
	bool,
	error,
) {
	u.lock.Lock()
	defer u.lock.Unlock()

	nodeID, ok := u.state.getNodeID(validationID)
	if !ok {
		return 0, false, nil
	}

	uptime, _, err := u.manager.CalculateUptime(nodeID)
	if err != nil {
		return 0, false, fmt.Errorf(
			"failed to calculate uptime for validator %s: %w",
			validationID,
			err,
		)
	}

	return uptime, true, nil
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
	if u.height < int(height) {
		if err := u.update(height, currentValidatorSet); err != nil {
			return fmt.Errorf("failed to update validator set: %w", err)
		}
	}

	// Initialize uptimes if this is the first time Sync has been called
	if !u.synced {
		validationIDs := make([]ids.NodeID, 0)
		for _, nodeID := range u.state.getNodeIDs() {
			validationIDs = append(validationIDs, nodeID)
		}

		if err := u.manager.StartTracking(validationIDs); err != nil {
			return fmt.Errorf("failed to start tracking validators: %w", err)
		}

		u.synced = true
	}

	if err := u.state.writeState(); err != nil {
		return fmt.Errorf("failed to write state: %w", err)
	}

	return nil
}

func (u *UptimeTracker) update(
	height uint64,
	currentValidatorSet map[ids.ID]*validators.GetCurrentValidatorOutput,
) error {
	currentValidationIDs := u.state.getValidationIDs()
	newValidators := currentValidatorSet

	for _, validationID := range currentValidationIDs {
		// This validator is still in the latest update
		if _, ok := newValidators[validationID]; ok {
			continue
		}

		// This validator was removed in the lastest update
		nodeID, ok := u.state.getNodeID(validationID)
		if !ok {
			return fmt.Errorf("failed to fetch validator %s", validationID)
		}

		if !u.state.deleteValidator(validationID) {
			return fmt.Errorf("failed to delete validator %s", validationID)
		}

		u.deactivatedValidators.Remove(nodeID)
	}

	// Add or update validators
	for validationID, next := range newValidators {
		if prev, ok := u.state.getValidatorByValidationID(validationID); ok {
			// We are updating a validator we know about
			isActiveUpdated := prev.IsActive == next.IsActive

			if err := u.state.updateValidator(
				validationID,
				next.IsActive,
				next.Weight,
			); err != nil {
				return fmt.Errorf(
					"failed to update validator %s: %w",
					validationID,
					err,
				)
			}

			// Check if the validator changed its status
			if isActiveUpdated {
				continue
			}

			if next.IsActive {
				// This validator is now active and is treated as online
				if err := u.activate(next.NodeID); err != nil {
					return fmt.Errorf(
						"failed to activate validator %s: %w",
						next.NodeID,
						err,
					)
				}
				continue
			}

			// This validator is no longer active and is treated as offline
			if err := u.deactivate(next.NodeID); err != nil {
				return fmt.Errorf(
					"failed to deactivate validator %s: %w",
					next.NodeID,
					err,
				)
			}
		} else {
			// This is a new validator
			if err := u.state.addValidatorUpdate(&validator{
				NodeID:        next.NodeID,
				validationID:  validationID,
				IsActive:      next.IsActive,
				StartTime:     next.StartTime,
				UpDuration:    0,
				LastUpdated:   next.StartTime,
				IsL1Validator: next.IsL1Validator,
				Weight:        next.Weight,
			}); err != nil {
				return fmt.Errorf(
					"failed to add validator %s: %w",
					next.NodeID,
					err,
				)
			}

			if next.IsActive {
				continue
			}

			// This validator is no longer active and is treated as offline
			if err := u.deactivate(next.NodeID); err != nil {
				return fmt.Errorf(
					"failed to deactivate validator %s: %w",
					next.NodeID,
					err,
				)
			}
		}
	}

	u.height = int(height)
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

	return u.state.writeState()
}
