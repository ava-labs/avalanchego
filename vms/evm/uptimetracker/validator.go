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
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"

	avalancheuptime "github.com/ava-labs/avalanchego/snow/uptime"
)

const (
	// SyncFrequency is the recommended frequency for calling Sync()
	// VMs can use this as a default but are free to choose their own timing
	SyncFrequency = 1 * time.Minute
)

// The ValidatorState struct is responsible for managing the state of validators by fetching
// information from P-Chain state (via GetCurrentValidatorSet in chain context) and updating
// the local state accordingly. The caller is responsible for calling Sync() periodically to
// keep the state up-to-date. The sync operation first removes validators that are no longer
// in the P-Chain validator set, then adds new validators and updates existing validators.
// This order of operations ensures that uptimes of validators being removed and re-added
// under the same nodeIDs are updated in the same sync operation despite having different
// validationIDs.
type ValidatorState struct {
	chainCtx        *snow.Context
	state           *state
	pausableManager pausableManager
}

// NewValidatorState returns a new validator state
// that manages the validator state and the uptime manager.
// ValidatorState is not thread safe and should be used with the VM locked.
func NewValidatorState(
	ctx *snow.Context,
	db database.Database,
	clock *mockable.Clock,
) (*ValidatorState, error) {
	validatorState, err := NewState(db)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize validator state: %w", err)
	}

	// Initialize uptime manager
	uptimeManager := NewPausableManager(avalancheuptime.NewManager(validatorState, clock))
	validatorState.SetPausableManager(uptimeManager)

	return &ValidatorState{
		chainCtx:        ctx,
		state:           validatorState,
		pausableManager: *uptimeManager,
	}, nil
}

// Shutdown stops uptime tracking and persists the validator state
func (m *ValidatorState) Shutdown() error {
	vdrIDs := m.state.GetNodeIDs().List()
	if err := m.pausableManager.manager.StopTracking(vdrIDs); err != nil {
		return fmt.Errorf("failed to stop uptime tracking: %w", err)
	}
	if !m.state.WriteState() {
		return errors.New("failed to write validator state")
	}

	return nil
}

// GetValidatorAndUptime returns the calculated uptime of the validator specified by [validationID]
// and the last updated time.
// GetValidatorAndUptime holds the lock while performing the operation and can be called concurrently.
func (m *ValidatorState) GetValidatorAndUptime(validationID ids.ID, lock sync.Locker) (Validator, time.Duration, time.Time, error) {
	lock.Lock()
	defer lock.Unlock()

	vdr, f := m.state.GetValidator(validationID)
	if !f {
		return Validator{}, 0, time.Time{}, fmt.Errorf("failed to get validator %s", validationID)
	}

	uptime, lastUpdated, err := m.pausableManager.manager.CalculateUptime(vdr.NodeID)
	if err != nil {
		return Validator{}, 0, time.Time{}, fmt.Errorf("failed to calculate uptime for validator %s: %w", validationID, err)
	}

	return vdr, uptime, lastUpdated, nil
}

// Sync synchronizes the validator state with the current validator set and writes the state to the database.
// Sync is not safe to call concurrently and should be called with the VM locked.
func (m *ValidatorState) Sync(ctx context.Context) error {
	start := time.Now()
	log.Debug("starting validator sync")

	// Get current validator set from P-Chain. P-Chain's `GetCurrentValidatorSet` can report both
	// L1 and Subnet validators. Subnet-EVM's uptime manager also tracks both of these validator
	// types. So even if a the Subnet has not yet been converted to an L1, the uptime and validator
	// state tracking is still performed by Subnet-EVM.
	currentValidatorSet, _, err := m.chainCtx.ValidatorState.GetCurrentValidatorSet(ctx, m.chainCtx.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to get current validator set: %w", err)
	}

	// Update local validator state
	if err := m.updateValidatorState(currentValidatorSet); err != nil {
		return fmt.Errorf("failed to update validator state: %w", err)
	}

	// ValidatorState persists the state to disk at the end of every sync operation. The VM also
	// persists the validator database when the node is shutting down.
	if !m.state.WriteState() {
		return errors.New("failed to write validator state")
	}

	// TODO: add metrics
	log.Debug("validator sync complete", "duration", time.Since(start))
	return nil
}

// updateValidatorState updates the local validator state to match the current validator set.
func (m *ValidatorState) updateValidatorState(newValidators map[ids.ID]*validators.GetCurrentValidatorOutput) error {
	currentValidationIDs := m.state.GetValidationIDs()

	// Remove validators no longer in the current set
	for vID := range currentValidationIDs {
		if _, exists := newValidators[vID]; !exists {
			if !m.state.DeleteValidator(vID) {
				return fmt.Errorf("failed to delete validator %s", vID)
			}
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
			if err := m.state.UpdateValidator(validator); err != nil {
				return fmt.Errorf("failed to update validator %s: %w", vID, err)
			}
		} else {
			if err := m.state.AddValidator(validator); err != nil {
				return fmt.Errorf("failed to add validator %s: %w", vID, err)
			}
		}
	}

	return nil
}
