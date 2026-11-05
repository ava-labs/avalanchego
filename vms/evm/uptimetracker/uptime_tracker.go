// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

var ErrValidationIDNotFound = errors.New("validationID not found")

// UptimeTracker tracks uptime information for validators
type UptimeTracker struct {
	validatorState validators.State
	subnetID       ids.ID
	manager        uptime.Manager

	lock   sync.Mutex
	height uint64
	state  *state
	synced bool
	online *onlineTracker
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

	manager := uptime.NewManager(s, clock)
	return &UptimeTracker{
		validatorState: validatorState,
		subnetID:       subnetID,
		manager:        manager,
		synced:         false,
		state:          s,
		online: &onlineTracker{
			manager: manager,
			state:   s,
		},
	}, nil
}

// GetUptime returns the uptime of the validator corresponding to validationID
func (u *UptimeTracker) GetUptime(validationID ids.ID) (
	time.Duration,
	time.Time,
	error,
) {
	u.lock.Lock()
	defer u.lock.Unlock()

	nodeID, ok := u.state.getNodeID(validationID)
	if !ok {
		return 0, time.Time{}, fmt.Errorf(
			"%w: %s",
			ErrValidationIDNotFound,
			validationID,
		)
	}

	uptime, lastUpdated, err := u.manager.CalculateUptime(nodeID)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf(
			"failed to calculate uptime for validator %s: %w",
			validationID,
			err,
		)
	}

	return uptime, lastUpdated, nil
}

// Connect tracks the node to earn uptime as long as it is not an inactive
// validator.
func (u *UptimeTracker) Connect(nodeID ids.NodeID) error {
	u.lock.Lock()
	defer u.lock.Unlock()

	return u.online.connect(nodeID)
}

// Disconnect marks a node as disconnected. Disconnected nodes are treated as
// offline and stop earning uptime.
func (u *UptimeTracker) Disconnect(nodeID ids.NodeID) error {
	u.lock.Lock()
	defer u.lock.Unlock()

	return u.online.disconnect(nodeID)
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

	// Update validator set if we're behind
	if err := u.update(height, currentValidatorSet); err != nil {
		return fmt.Errorf("failed to update validator set: %w", err)
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
	// We are behind and need to update our local state
	if u.synced && u.height >= height {
		return nil
	}

	u.state.setValidators(currentValidatorSet)
	if err := u.online.reconcileAll(); err != nil {
		return err
	}

	u.height = height
	return nil
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
