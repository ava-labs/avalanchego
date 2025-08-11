// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/libevm/log"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/evm/plugin/validators/state"
	"github.com/ava-labs/avalanchego/vms/evm/plugin/validators/uptime"

	avalancheuptime "github.com/ava-labs/avalanchego/snow/uptime"
	stateinterfaces "github.com/ava-labs/avalanchego/vms/evm/plugin/validators/state/interfaces"
	uptimeinterfaces "github.com/ava-labs/avalanchego/vms/evm/plugin/validators/uptime/interfaces"
)

const (
	SyncFrequency = 1 * time.Minute
)

type manager struct {
	chainCtx *snow.Context
	stateinterfaces.State
	uptimeinterfaces.PausableManager
}

// NewManager returns a new validator manager
// that manages the validator state and the uptime manager.
// Manager is not thread safe and should be used with the VM locked.
func NewManager(
	ctx *snow.Context,
	db database.Database,
	clock *mockable.Clock,
) (*manager, error) {
	validatorState, err := state.NewState(db)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize validator state: %w", err)
	}

	// Initialize uptime manager
	uptimeManager := uptime.NewPausableManager(avalancheuptime.NewManager(validatorState, clock))
	validatorState.RegisterListener(uptimeManager)

	return &manager{
		chainCtx:        ctx,
		State:           validatorState,
		PausableManager: uptimeManager,
	}, nil
}

// Initialize initializes the validator manager
// by syncing the validator state with the current validator set
// and starting the uptime tracking.
func (m *manager) Initialize(ctx context.Context) error {
	// sync validators first
	if err := m.sync(ctx); err != nil {
		return fmt.Errorf("failed to sync validators: %w", err)
	}

	vdrIDs := m.State.GetNodeIDs().List()
	// Then start tracking with updated validators
	// StartTracking initializes the uptime tracking with the known validators
	// and update their uptime to account for the time we were being offline.
	if err := m.PausableManager.StartTracking(vdrIDs); err != nil {
		return fmt.Errorf("failed to start uptime tracking: %w", err)
	}

	return nil
}

// Shutdown stops uptime tracking and persists validator state
func (m *manager) Shutdown() error {
	vdrIDs := m.State.GetNodeIDs().List()
	if err := m.PausableManager.StopTracking(vdrIDs); err != nil {
		return fmt.Errorf("failed to stop uptime tracking: %w", err)
	}
	if err := m.State.WriteState(); err != nil {
		return fmt.Errorf("failed to write validator state: %w", err)
	}

	return nil
}

// DispatchSync starts the sync process
// DispatchSync holds the given lock while performing the sync.
func (m *manager) DispatchSync(ctx context.Context, lock sync.Locker) {
	ticker := time.NewTicker(SyncFrequency)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lock.Lock()
			if err := m.sync(ctx); err != nil {
				log.Error("failed to sync validators", "error", err)
			}
			lock.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

// sync synchronizes the validator state with the current validator set
// and writes the state to the database.
// sync is not safe to call concurrently and should be called with the VM locked.
func (m *manager) sync(ctx context.Context) error {
	start := time.Now()
	log.Debug("starting validator sync")

	// Get current validator set from P-Chain
	currentValidatorSet, _, err := m.chainCtx.ValidatorState.GetCurrentValidatorSet(ctx, m.chainCtx.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to get current validator set: %w", err)
	}

	// Update local validator state
	if err := m.updateValidatorState(currentValidatorSet); err != nil {
		return fmt.Errorf("failed to update validator state: %w", err)
	}

	// Persist changes
	if err := m.State.WriteState(); err != nil {
		return fmt.Errorf("failed to write validator state: %w", err)
	}

	// TODO: add metrics
	log.Debug("validator sync complete", "duration", time.Since(start))
	return nil
}

// updateValidatorState updates the local validator state to match the current validator set
func (m *manager) updateValidatorState(newValidators map[ids.ID]*validators.GetCurrentValidatorOutput) error {
	currentValidationIDs := m.State.GetValidationIDs()

	// Remove validators no longer in the current set
	for vID := range currentValidationIDs {
		if _, exists := newValidators[vID]; !exists {
			if err := m.State.DeleteValidator(vID); err != nil {
				return fmt.Errorf("failed to delete validator %s: %w", vID, err)
			}
		}
	}

	// Add or update validators
	for vID, newVdr := range newValidators {
		validator := stateinterfaces.Validator{
			ValidationID:   vID,
			NodeID:         newVdr.NodeID,
			Weight:         newVdr.Weight,
			StartTimestamp: newVdr.StartTime,
			IsActive:       newVdr.IsActive,
			IsL1Validator:  newVdr.IsL1Validator,
		}

		if currentValidationIDs.Contains(vID) {
			if err := m.State.UpdateValidator(validator); err != nil {
				return fmt.Errorf("failed to update validator %s: %w", vID, err)
			}
		} else {
			if err := m.State.AddValidator(validator); err != nil {
				return fmt.Errorf("failed to add validator %s: %w", vID, err)
			}
		}
	}

	return nil
}
