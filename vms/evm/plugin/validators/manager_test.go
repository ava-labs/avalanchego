// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/vms/evm/plugin/validators/state"
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

	testCases := []struct {
		name                      string
		initialValidators         map[ids.ID]*validators.GetCurrentValidatorOutput
		newValidators             map[ids.ID]*validators.GetCurrentValidatorOutput
		registerMockListenerCalls func(*state.MockStateCallbackListener)
		wantLoadErr               error
	}{
		{
			name:                      "before empty/after empty",
			initialValidators:         map[ids.ID]*validators.GetCurrentValidatorOutput{},
			newValidators:             map[ids.ID]*validators.GetCurrentValidatorOutput{},
			registerMockListenerCalls: func(*state.MockStateCallbackListener) {},
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
			registerMockListenerCalls: func(mock *state.MockStateCallbackListener) {
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
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
			registerMockListenerCalls: func(mock *state.MockStateCallbackListener) {
				// initial validator will trigger first
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
				// then it will be removed
				mock.EXPECT().OnValidatorRemoved(testValidationIDs[0], testNodeIDs[0]).Times(1)
			},
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
			registerMockListenerCalls: func(mock *state.MockStateCallbackListener) {
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
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
			registerMockListenerCalls: func(mock *state.MockStateCallbackListener) {
				// initial validator will trigger first
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
				// then it will be updated
				mock.EXPECT().OnValidatorStatusUpdated(testValidationIDs[0], testNodeIDs[0], false).Times(1)
				// new validator will be added
				mock.EXPECT().OnValidatorAdded(testValidationIDs[1], testNodeIDs[1], uint64(0), true).Times(1)
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
			registerMockListenerCalls: func(mock *state.MockStateCallbackListener) {
				// initial validator will trigger first
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
				// then it will be removed
				mock.EXPECT().OnValidatorRemoved(testValidationIDs[0], testNodeIDs[0]).Times(1)
				// new validator will be added
				mock.EXPECT().OnValidatorAdded(testValidationIDs[1], testNodeIDs[0], uint64(0), true).Times(1)
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
			wantLoadErr: state.ErrImmutableField,
			registerMockListenerCalls: func(mock *state.MockStateCallbackListener) {
				// initial validator will trigger first
				mock.EXPECT().OnValidatorAdded(testValidationIDs[0], testNodeIDs[0], uint64(0), true).Times(1)
				// then it won't be called since we don't track the node ID changes
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(tt *testing.T) {
			require := require.New(tt)
			db := memdb.New()
			validatorState, err := state.NewState(db)
			require.NoError(err)

			// Set initial validators
			for vID, validator := range tc.initialValidators {
				require.NoError(validatorState.AddValidator(state.Validator{
					ValidationID:   vID,
					NodeID:         validator.NodeID,
					Weight:         validator.Weight,
					StartTimestamp: validator.StartTime,
					IsActive:       validator.IsActive,
					IsL1Validator:  validator.IsL1Validator,
				}))
			}

			// Enable mock listener
			ctrl := gomock.NewController(tt)
			mockListener := state.NewMockStateCallbackListener(ctrl)
			tc.registerMockListenerCalls(mockListener)

			validatorState.RegisterListener(mockListener)

			// Load new validators using the same logic as the manager
			err = loadValidatorsForTest(validatorState, tc.newValidators)
			if tc.wantLoadErr != nil {
				require.ErrorIs(err, tc.wantLoadErr)
				return
			}
			require.NoError(err)

			// Verify final state matches expectations
			require.Equal(len(tc.newValidators), validatorState.GetValidationIDs().Len())
			for vID, validator := range tc.newValidators {
				v, err := validatorState.GetValidator(vID)
				require.NoError(err)
				require.Equal(validator.NodeID, v.NodeID)
				require.Equal(validator.Weight, v.Weight)
				require.Equal(validator.StartTime, v.StartTimestamp)
				require.Equal(validator.IsActive, v.IsActive)
				require.Equal(validator.IsL1Validator, v.IsL1Validator)
			}
		})
	}
}

// loadValidatorsForTest is a test helper that replicates the logic from the manager
// for testing purposes
func loadValidatorsForTest(validatorState *state.State, newValidators map[ids.ID]*validators.GetCurrentValidatorOutput) error {
	currentValidationIDs := validatorState.GetValidationIDs()

	// Remove validators no longer in the current set
	for vID := range currentValidationIDs {
		if _, exists := newValidators[vID]; !exists {
			if err := validatorState.DeleteValidator(vID); err != nil {
				return err
			}
		}
	}

	// Add or update validators
	for vID, newVdr := range newValidators {
		validator := state.Validator{
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
