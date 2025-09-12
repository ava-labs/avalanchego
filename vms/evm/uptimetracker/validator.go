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
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

const (
	// SyncFrequency is the recommended frequency for calling Sync()
	// VMs can use this as a default but are free to choose their own timing
	SyncFrequency = 1 * time.Minute
)

// The UptimeTracker struct is responsible for managing the state of validators by fetching
// information from P-Chain state (via GetCurrentValidatorSet in chain context) and updating
// the local state accordingly. The caller is responsible for calling Sync() periodically to
// keep the state up-to-date. The sync operation first removes validators that are no longer
// in the P-Chain validator set, then adds new validators and updates existing validators.
// This order of operations ensures that uptimes of validators being removed and re-added
// under the same nodeIDs are updated in the same sync operation despite having different
// validationIDs.
type UptimeTracker struct {
	chainCtx        *snow.Context
	state           *state
	pausableManager pausableManager
}

// NewUptimeTracker returns a new validator state
// that manages the validator state and the uptime manager.
// ValidatorState is not thread safe and should be used with the VM locked.
func NewUptimeTracker(
	ctx *snow.Context,
	db database.Database,
	clock *mockable.Clock,
) (*UptimeTracker, error) {
	validatorState, err := newState(db)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize validator state: %w", err)
	}

	// Initialize uptime manager
	uptimeManager := newPausableManager(uptime.NewManager(validatorState, clock))

	return &UptimeTracker{
		chainCtx:        ctx,
		state:           validatorState,
		pausableManager: *uptimeManager,
	}, nil
}

// GetValidationIDs returns the validation IDs in the state
// The implementation tracks validators by their validationIDs and assumes
// they're unique per node and their validation period.
func (u *UptimeTracker) GetValidationIDs() set.Set[ids.ID] {
	ids := set.NewSet[ids.ID](len(u.state.data))
	for vID := range u.state.data {
		ids.Add(vID)
	}
	return ids
}

// GetNodeIDs returns the node IDs of validators in the state
func (u *UptimeTracker) GetNodeIDs() set.Set[ids.NodeID] {
	ids := set.NewSet[ids.NodeID](len(u.state.index))
	for nodeID := range u.state.index {
		ids.Add(nodeID)
	}
	return ids
}

// GetValidationID returns the validation ID for the given nodeID
func (u *UptimeTracker) GetValidationID(nodeID ids.NodeID) (ids.ID, bool) {
	vID, exists := u.state.index[nodeID]
	if !exists {
		return ids.ID{}, false
	}
	return vID, true
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

// Connect connects the given node to uptime tracking (no-op if paused)
func (u *UptimeTracker) Connect(nodeID ids.NodeID) error {
	return u.pausableManager.connect(nodeID)
}

// Disconnect disconnects the given node from uptime tracking
func (u *UptimeTracker) Disconnect(nodeID ids.NodeID) error {
	return u.pausableManager.disconnect(nodeID)
}

// Shutdown stops uptime tracking and persists the validator state
func (u *UptimeTracker) Shutdown() error {
	vdrIDs := u.GetNodeIDs().List()
	if err := u.pausableManager.manager.StopTracking(vdrIDs); err != nil {
		return fmt.Errorf("failed to stop uptime tracking: %w", err)
	}
	if !u.state.writeState() {
		return errors.New("failed to write validator state")
	}

	return nil
}

// GetValidatorAndUptime returns the calculated uptime of the validator specified by [validationID]
// and the last updated time.
// GetValidatorAndUptime holds the lock while performing the operation and can be called concurrently.
func (u *UptimeTracker) GetValidatorAndUptime(validationID ids.ID, lock sync.Locker) (Validator, time.Duration, time.Time, error) {
	lock.Lock()
	defer lock.Unlock()

	vdr, f := u.GetValidator(validationID)
	if !f {
		return Validator{}, 0, time.Time{}, fmt.Errorf("failed to get validator %s", validationID)
	}

	uptime, lastUpdated, err := u.pausableManager.manager.CalculateUptime(vdr.NodeID)
	if err != nil {
		return Validator{}, 0, time.Time{}, fmt.Errorf("failed to calculate uptime for validator %s: %w", validationID, err)
	}

	return vdr, uptime, lastUpdated, nil
}

// Sync synchronizes the validator state with the current validator set and writes the state to the database.
// Sync is not safe to call concurrently and should be called with the VM locked.
func (u *UptimeTracker) Sync(ctx context.Context) error {
	start := time.Now()
	log.Debug("starting validator sync")

	// Get current validator set from P-Chain. P-Chain's `GetCurrentValidatorSet` can report both
	// L1 and Subnet validators. Subnet-EVM's uptime manager also tracks both of these validator
	// types. So even if a the Subnet has not yet been converted to an L1, the uptime and validator
	// state tracking is still performed by Subnet-EVM.
	currentValidatorSet, _, err := u.chainCtx.ValidatorState.GetCurrentValidatorSet(ctx, u.chainCtx.SubnetID)
	if err != nil {
		return fmt.Errorf("failed to get current validator set: %w", err)
	}

	// Update local validator state
	if err := u.updateValidatorState(currentValidatorSet); err != nil {
		return fmt.Errorf("failed to update validator state: %w", err)
	}

	// ValidatorState persists the state to disk at the end of every sync operation. The VM also
	// persists the validator database when the node is shutting down.
	if !u.state.writeState() {
		return errors.New("failed to write validator state")
	}

	// TODO: add metrics
	log.Debug("validator sync complete", "duration", time.Since(start))
	return nil
}

// updateValidatorState updates the local validator state to match the current validator set.
func (u *UptimeTracker) updateValidatorState(newValidators map[ids.ID]*validators.GetCurrentValidatorOutput) error {
	currentValidationIDs := u.GetValidationIDs()

	// Remove validators no longer in the current set
	for vID := range currentValidationIDs {
		if _, exists := newValidators[vID]; !exists {
			// fetch validator for nodeID prior to deletion
			existing, ok := u.GetValidator(vID)
			if !ok {
				return fmt.Errorf("failed to fetch validator %s", vID)
			}
			if !u.state.deleteValidator(vID) {
				return fmt.Errorf("failed to delete validator %s", vID)
			}
			u.pausableManager.onValidatorRemoved(vID, existing.NodeID)
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
			if prev.IsActive != validator.IsActive {
				u.pausableManager.onValidatorStatusUpdated(vID, validator.NodeID, validator.IsActive)
			}
		} else {
			if err := u.state.addValidator(validator); err != nil {
				return fmt.Errorf("failed to add validator %s: %w", vID, err)
			}
			u.pausableManager.onValidatorAdded(vID, validator.NodeID, validator.StartTimestamp, validator.IsActive)
		}
	}

	return nil
}
