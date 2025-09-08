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
		name              string
		initialValidators map[ids.ID]*validators.GetCurrentValidatorOutput
		newValidators     map[ids.ID]*validators.GetCurrentValidatorOutput
		wantLoadErr       error
	}{
		{
			name:              "before empty/after empty",
			initialValidators: map[ids.ID]*validators.GetCurrentValidatorOutput{},
			newValidators:     map[ids.ID]*validators.GetCurrentValidatorOutput{},
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			db := memdb.New()
			validatorState, err := newState(db)
			require.NoError(err)

			// Set initial validators
			for vID, validator := range tt.initialValidators {
				require.NoError(validatorState.addValidator(Validator{
					ValidationID:   vID,
					NodeID:         validator.NodeID,
					Weight:         validator.Weight,
					StartTimestamp: validator.StartTime,
					IsActive:       validator.IsActive,
					IsL1Validator:  validator.IsL1Validator,
				}))
			}

			// Load new validators using the same logic as the manager
			err = loadValidatorsForTest(validatorState, tt.newValidators)
			if tt.wantLoadErr != nil {
				require.ErrorIs(err, tt.wantLoadErr)
				return
			}
			require.NoError(err)

			// Verify final state matches expectations by inspecting validatorState directly
			require.Equal(len(tt.newValidators), len(validatorState.data))
			for vID, newVdr := range tt.newValidators {
				vdrData, ok := validatorState.data[vID]
				require.True(ok)
				require.Equal(newVdr.NodeID, vdrData.NodeID)
				require.Equal(newVdr.Weight, vdrData.Weight)
				require.Equal(newVdr.StartTime, vdrData.StartTime)
				require.Equal(newVdr.IsActive, vdrData.IsActive)
				require.Equal(newVdr.IsL1Validator, vdrData.IsL1Validator)
			}
		})
	}
}

// loadValidatorsForTest is a test helper that replicates the logic from the manager
// for testing purposes
func loadValidatorsForTest(validatorState *state, newValidators map[ids.ID]*validators.GetCurrentValidatorOutput) error {
	// Remove validators no longer in the current set
	for existingVID := range validatorState.data {
		if _, exists := newValidators[existingVID]; !exists {
			if !validatorState.deleteValidator(existingVID) {
				return fmt.Errorf("failed to find validator %s", existingVID)
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
		if _, exists := validatorState.data[vID]; exists {
			if err := validatorState.updateValidator(validator); err != nil {
				return err
			}
		} else {
			if err := validatorState.addValidator(validator); err != nil {
				return err
			}
		}
	}

	return nil
}
