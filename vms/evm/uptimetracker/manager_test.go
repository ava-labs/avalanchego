// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestLoadNewValidators(t *testing.T) {
	testNodeIDs := []ids.NodeID{
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
		ids.GenerateTestNodeID(),
	}
	testValidationIDs := []ids.ID{
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		ids.GenerateTestID(),
	}

	tests := []struct {
		name                  string
		initialValidators     map[ids.ID]*validators.GetCurrentValidatorOutput
		newValidators         map[ids.ID]*validators.GetCurrentValidatorOutput
		wantLoadErr           error
		wantAddedValidators   map[ids.ID]ids.NodeID
		wantRemovedValidators map[ids.ID]ids.NodeID
		wantStatusUpdates     map[ids.ID]bool
	}{
		{
			name:                  "before empty/after empty",
			initialValidators:     map[ids.ID]*validators.GetCurrentValidatorOutput{},
			newValidators:         map[ids.ID]*validators.GetCurrentValidatorOutput{},
			wantAddedValidators:   map[ids.ID]ids.NodeID{},
			wantRemovedValidators: map[ids.ID]ids.NodeID{},
			wantStatusUpdates:     map[ids.ID]bool{},
		},
		{
			name:              "before empty/after one",
			initialValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{},
			newValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			wantAddedValidators: map[ids.ID]ids.NodeID{
				testValidationIDs[0]: testNodeIDs[0],
			},
			wantRemovedValidators: map[ids.ID]ids.NodeID{},
			wantStatusUpdates:     map[ids.ID]bool{},
		},
		{
			name: "before one/after empty",
			initialValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			newValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{},
			wantAddedValidators: map[ids.ID]ids.NodeID{
				testValidationIDs[0]: testNodeIDs[0],
			},
			wantRemovedValidators: map[ids.ID]ids.NodeID{
				testValidationIDs[0]: testNodeIDs[0],
			},
			wantStatusUpdates: map[ids.ID]bool{},
		},
		{
			name: "no change",
			initialValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			newValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			wantAddedValidators: map[ids.ID]ids.NodeID{
				testValidationIDs[0]: testNodeIDs[0],
			},
			wantRemovedValidators: map[ids.ID]ids.NodeID{},
			wantStatusUpdates:     map[ids.ID]bool{},
		},
		{
			name: "status and weight change and new one",
			initialValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
					Weight:    1,
				},
			},
			newValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  false,
					StartTime: 0,
					Weight:    2,
				},
				testValidationIDs[1]: {
					NodeID:    testNodeIDs[1],
					IsActive:  true,
					StartTime: 0,
				},
			},
			wantAddedValidators: map[ids.ID]ids.NodeID{
				testValidationIDs[0]: testNodeIDs[0],
				testValidationIDs[1]: testNodeIDs[1],
			},
			wantRemovedValidators: map[ids.ID]ids.NodeID{},
			wantStatusUpdates: map[ids.ID]bool{
				testValidationIDs[0]: false,
			},
		},
		{
			name: "renew validation ID",
			initialValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			newValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[1]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			wantAddedValidators: map[ids.ID]ids.NodeID{
				testValidationIDs[0]: testNodeIDs[0], // Initial validator
				testValidationIDs[1]: testNodeIDs[0], // New validator
			},
			wantRemovedValidators: map[ids.ID]ids.NodeID{
				testValidationIDs[0]: testNodeIDs[0], // Old validator removed
			},
			wantStatusUpdates: map[ids.ID]bool{},
		},
		{
			name: "renew node ID",
			initialValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[0],
					IsActive:  true,
					StartTime: 0,
				},
			},
			newValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{
				testValidationIDs[0]: {
					NodeID:    testNodeIDs[1],
					IsActive:  true,
					StartTime: 0,
				},
			},
			wantLoadErr: ErrImmutableField,
			wantAddedValidators: map[ids.ID]ids.NodeID{
				testValidationIDs[0]: testNodeIDs[0], // Only initial validator
			},
			wantRemovedValidators: map[ids.ID]ids.NodeID{},
			wantStatusUpdates:     map[ids.ID]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			db := memdb.New()
			validatorState, err := NewState(db)
			require.NoError(err)

			// Set initial validators
			for vID, validator := range tt.initialValidators {
				require.NoError(validatorState.AddValidator(Validator{
					ValidationID:   vID,
					NodeID:         validator.NodeID,
					Weight:         validator.Weight,
					StartTimestamp: validator.StartTime,
					IsActive:       validator.IsActive,
					IsL1Validator:  validator.IsL1Validator,
				}))
			}

			// Enable test pausable manager to track callbacks
			testManager := NewTestPausableManager()
			validatorState.SetCallbacks(
				testManager.OnValidatorAdded,
				testManager.OnValidatorRemoved,
				testManager.OnValidatorStatusUpdated,
			)

			// Load new validators using the same logic as the manager
			err = loadValidatorsForTest(validatorState, tt.newValidators)
			if tt.wantLoadErr != nil {
				require.ErrorIs(err, tt.wantLoadErr)
				return
			}
			require.NoError(err)

			// Verify final state matches expectations
			require.Equal(len(tt.newValidators), validatorState.GetValidationIDs().Len())
			for vID, validator := range tt.newValidators {
				v, f := validatorState.GetValidator(vID)
				require.True(f)
				require.Equal(validator.NodeID, v.NodeID)
				require.Equal(validator.Weight, v.Weight)
				require.Equal(validator.StartTime, v.StartTimestamp)
				require.Equal(validator.IsActive, v.IsActive)
				require.Equal(validator.IsL1Validator, v.IsL1Validator)
			}

			// Verify callback tracking worked correctly
			require.Equal(tt.wantAddedValidators, testManager.AddedValidators)
			require.Equal(tt.wantRemovedValidators, testManager.RemovedValidators)
			require.Equal(tt.wantStatusUpdates, testManager.StatusUpdates)
		})
	}
}

// loadValidatorsForTest is a test helper that replicates the logic from the manager
// for testing purposes
func loadValidatorsForTest(validatorState *state, newValidators map[ids.ID]*validators.GetCurrentValidatorOutput) error {
	currentValidationIDs := validatorState.GetValidationIDs()

	// Remove validators no longer in the current set
	for vID := range currentValidationIDs {
		if _, exists := newValidators[vID]; !exists {
			if !validatorState.DeleteValidator(vID) {
				return fmt.Errorf("failed to find validator %s", vID)
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
			if err := validatorState.UpdateValidator(validator); err != nil {
				return err
			}
		} else {
			if err := validatorState.AddValidator(validator); err != nil {
				return err
			}
		}
	}

	return nil
}
