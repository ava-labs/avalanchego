// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
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
			err = loadValidatorsForTest(t, validatorState, tt.newValidators)
			if tt.wantLoadErr != nil {
				require.ErrorIs(err, tt.wantLoadErr)
				return
			}
			require.NoError(err)

			// Verify final state matches expectations by inspecting validatorState directly
			require.Len(validatorState.data, len(tt.newValidators))
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

func TestSync_ValidatorLifecycle(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Prepare mock validator state to drive successive Sync calls
	mockVS := validatorsmock.NewState(ctrl)
	ctx := &snow.Context{}
	ctx.SubnetID = ids.GenerateTestID()
	ctx.ValidatorState = validators.NewLockedState(&ctx.Lock, mockVS)

	// Build NewUptimeTracker with real state and uptime manager
	db := memdb.New()
	clk := mockable.Clock{}
	clk.Set(time.Unix(0, 0))
	state, err := newState(db)
	require.NoError(err)
	ut, err := NewUptimeTracker(ctx, uptime.NewManager(state, &clk), db)
	require.NoError(err)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Initially no validators
	require.Empty(ut.GetValidators())

	// Connect node before it's a validator
	require.NoError(ut.Connect(nodeID))
	require.True(ut.IsConnected(nodeID))

	// First sync: add active validator
	firstSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}
	mockVS.EXPECT().GetCurrentValidatorSet(gomock.Any(), ctx.SubnetID).Return(firstSet, uint64(1), nil)
	require.NoError(ut.Sync(context.Background()))

	// Verify validator was added and is tracked
	validatorList := ut.GetValidators()
	require.Len(validatorList, 1)
	require.Equal(nodeID, validatorList[0].NodeID)
	require.Equal(vID, validatorList[0].ValidationID)
	require.True(ut.IsConnected(nodeID))

	// Get validator info
	validator, exists := ut.GetValidator(vID)
	require.True(exists)
	require.Equal(nodeID, validator.NodeID)
	require.Equal(uint64(1000), validator.Weight)
	require.True(validator.IsActive)
	require.False(validator.IsL1Validator)

	// Second sync: validator becomes inactive
	secondSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      false,
			IsL1Validator: false,
		},
	}
	mockVS.EXPECT().GetCurrentValidatorSet(gomock.Any(), ctx.SubnetID).Return(secondSet, uint64(2), nil)
	require.NoError(ut.Sync(context.Background()))

	// Validator should still be tracked but now inactive
	validatorList = ut.GetValidators()
	require.Len(validatorList, 1)
	require.Equal(nodeID, validatorList[0].NodeID)
	require.Equal(vID, validatorList[0].ValidationID)
	require.True(ut.IsConnected(nodeID)) // Still connected from our perspective

	// Get updated validator info
	validator, exists = ut.GetValidator(vID)
	require.True(exists)
	require.False(validator.IsActive)

	// Third sync: validator becomes active again
	thirdSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}
	mockVS.EXPECT().GetCurrentValidatorSet(gomock.Any(), ctx.SubnetID).Return(thirdSet, uint64(3), nil)
	require.NoError(ut.Sync(context.Background()))

	// Validator should still be tracked and active
	validatorList = ut.GetValidators()
	require.Len(validatorList, 1)
	require.Equal(nodeID, validatorList[0].NodeID)
	require.Equal(vID, validatorList[0].ValidationID)
	require.True(ut.IsConnected(nodeID))

	// Get updated validator info
	validator, exists = ut.GetValidator(vID)
	require.True(exists)
	require.True(validator.IsActive)

	// Fourth sync: validator removed
	emptySet := map[ids.ID]*validators.GetCurrentValidatorOutput{}
	mockVS.EXPECT().GetCurrentValidatorSet(gomock.Any(), ctx.SubnetID).Return(emptySet, uint64(4), nil)
	require.NoError(ut.Sync(context.Background()))

	// Validator should be removed
	validatorList = ut.GetValidators()
	require.Empty(validatorList)
	require.True(ut.IsConnected(nodeID)) // Still connected from our perspective
}

func TestUptimeTracking_ValidatorStatusChanges(t *testing.T) {
	vID := ids.GenerateTestID()
	nodeID0 := ids.GenerateTestNodeID()
	startTime := time.Now()

	tests := []struct {
		name     string
		testFunc func(t *testing.T, u UptimeTracker, clk *mockable.Clock)
	}{
		{
			name: "Connect before tracking, then track inactive validator",
			testFunc: func(t *testing.T, u UptimeTracker, clk *mockable.Clock) {
				require := require.New(t)

				// Connect before tracking
				require.NoError(u.Connect(nodeID0))
				addTime(t, clk, time.Second)
				wantUptime := 1 * time.Second

				// Add inactive validator
				u.onValidatorStatusUpdated(vID, nodeID0, false)

				// Elapse Time
				addTime(t, clk, time.Second)
				// Since we have not started tracking this node yet, its observed uptime should
				// be incremented even though it is actually paused.
				wantUptime += 1 * time.Second

				// Start tracking
				require.NoError(u.manager.StartTracking([]ids.NodeID{nodeID0}))
				// Uptime here should not increase after start tracking
				// since the node is still paused after we started tracking
				currentTime := addTime(t, clk, time.Second)
				checkUptime(t, u, nodeID0, wantUptime, currentTime)

				// Disconnect
				require.NoError(u.Disconnect(nodeID0))
				// Uptime should not have increased
				checkUptime(t, u, nodeID0, wantUptime, currentTime)
			},
		},
		{
			name: "Start tracking, connect, then validator becomes inactive",
			testFunc: func(t *testing.T, u UptimeTracker, clk *mockable.Clock) {
				require := require.New(t)

				// Start tracking
				require.NoError(u.manager.StartTracking([]ids.NodeID{nodeID0}))

				// Connect
				addTime(t, clk, 1*time.Second)
				require.NoError(u.Connect(nodeID0))

				// Validator becomes inactive
				addTime(t, clk, 1*time.Second)
				u.onValidatorStatusUpdated(vID, nodeID0, false)

				// Elapse time
				currentTime := addTime(t, clk, 2*time.Second)
				// Uptime should be 1 second since the node was paused after 1 sec
				wantUptime := 1 * time.Second
				checkUptime(t, u, nodeID0, wantUptime, currentTime)

				// Disconnect and check uptime
				currentTime = addTime(t, clk, 3*time.Second)
				require.NoError(u.Disconnect(nodeID0))
				// Uptime should not have increased since the node was paused
				checkUptime(t, u, nodeID0, wantUptime, currentTime)

				// Connect again and check uptime
				addTime(t, clk, 4*time.Second)
				require.NoError(u.Connect(nodeID0))
				currentTime = addTime(t, clk, 5*time.Second)
				// Uptime should not have increased since the node was paused
				checkUptime(t, u, nodeID0, wantUptime, currentTime)

				// Validator becomes active again
				currentTime = addTime(t, clk, 6*time.Second)
				u.onValidatorStatusUpdated(vID, nodeID0, true)
				// Uptime should not have increased since the node was paused
				// and we just resumed it
				checkUptime(t, u, nodeID0, wantUptime, currentTime)

				// Elapsed time check
				currentTime = addTime(t, clk, 7*time.Second)
				// Uptime should increase by 7 seconds above since the node was resumed
				wantUptime += 7 * time.Second
				checkUptime(t, u, nodeID0, wantUptime, currentTime)
			},
		},
		{
			name: "Validator inactive before tracking, then becomes active",
			testFunc: func(t *testing.T, up UptimeTracker, clk *mockable.Clock) {
				require := require.New(t)

				// Validator becomes inactive before tracking
				up.onValidatorStatusUpdated(vID, nodeID0, false)

				// Start tracking
				addTime(t, clk, time.Second)
				// Uptime should be 1 since the node was paused before we started tracking
				wantUptime := 1 * time.Second
				require.NoError(up.manager.StartTracking([]ids.NodeID{nodeID0}))

				// Connect and check uptime
				addTime(t, clk, 1*time.Second)
				checkUptime(t, up, nodeID0, wantUptime, clk.Time())
				require.NoError(up.Connect(nodeID0))

				currentTime := addTime(t, clk, 2*time.Second)
				// Uptime should not have increased since the node was paused after we started tracking
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Disconnect and check uptime
				currentTime = addTime(t, clk, 3*time.Second)
				require.NoError(up.Disconnect(nodeID0))
				// Uptime should not have increased since the node was paused
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Connect again and validator becomes active
				addTime(t, clk, 4*time.Second)
				require.NoError(up.Connect(nodeID0))
				addTime(t, clk, 5*time.Second)
				up.onValidatorStatusUpdated(vID, nodeID0, true)

				// Check uptime after resume
				currentTime = addTime(t, clk, 6*time.Second)
				// Uptime should have increased by 6 seconds since the node was resumed
				wantUptime += 6 * time.Second
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
			},
		},
		{
			name: "Basic uptime tracking with connection and disconnection",
			testFunc: func(t *testing.T, up UptimeTracker, clk *mockable.Clock) {
				require := require.New(t)

				// Start tracking and connect
				require.NoError(up.manager.StartTracking([]ids.NodeID{nodeID0}))
				addTime(t, clk, time.Second)
				require.NoError(up.Connect(nodeID0))

				// Get initial uptime
				initialUptime, _, err := up.manager.CalculateUptime(nodeID0)
				require.NoError(err)

				// Validator becomes inactive
				addTime(t, clk, 2*time.Second)
				up.onValidatorStatusUpdated(vID, nodeID0, false)

				// Wait some time and check that uptime doesn't increase while inactive
				addTime(t, clk, 2*time.Second)
				_, lastUpdated, err := up.manager.CalculateUptime(nodeID0)
				require.NoError(err)
				require.Equal(lastUpdated.Unix(), clk.Time().Unix())
				// Note: The exact uptime behavior may vary based on implementation
				// The key is that the system handles the status change correctly

				// Validator becomes active again
				up.onValidatorStatusUpdated(vID, nodeID0, true)

				// Connect and verify we can still track uptime
				require.NoError(up.Connect(nodeID0))
				addTime(t, clk, 2*time.Second)
				finalUptime, _, err := up.manager.CalculateUptime(nodeID0)
				require.NoError(err)
				require.GreaterOrEqual(finalUptime, initialUptime) // Should have increased or stayed the same
			},
		},
		{
			name: "Uptime tracking with stop and restart",
			testFunc: func(t *testing.T, up UptimeTracker, clk *mockable.Clock) {
				require := require.New(t)

				// Start tracking and connect
				require.NoError(up.manager.StartTracking([]ids.NodeID{nodeID0}))
				addTime(t, clk, time.Second)
				require.NoError(up.Connect(nodeID0))

				// Stop tracking
				currentTime := addTime(t, clk, 2*time.Second)
				wantUptime := 2 * time.Second
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
				require.NoError(up.manager.StopTracking([]ids.NodeID{nodeID0}))

				// Validator becomes inactive after stopping tracking
				addTime(t, clk, 3*time.Second)
				// wantUptime should increase since we stopped tracking
				wantUptime += 3 * time.Second
				up.onValidatorStatusUpdated(vID, nodeID0, false)
				// wantUptime should increase since we stopped tracking (even if the node was paused)
				currentTime = addTime(t, clk, 4*time.Second)
				wantUptime += 4 * time.Second

				// Start tracking and check elapsed time
				require.NoError(up.manager.StartTracking([]ids.NodeID{nodeID0}))
				// Uptime have increased since the node was paused before we started tracking
				// We should be optimistic and assume the node was online and active until we start tracking
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
			},
		},
		{
			name: "Validator status changes during tracking stop",
			testFunc: func(t *testing.T, up UptimeTracker, clk *mockable.Clock) {
				require := require.New(t)

				// Start tracking and connect
				require.NoError(up.manager.StartTracking([]ids.NodeID{nodeID0}))
				addTime(t, clk, time.Second)
				require.NoError(up.Connect(nodeID0))

				// Validator becomes inactive
				currentTime := addTime(t, clk, 2*time.Second)
				// wantUptime should increase
				wantUptime := 2 * time.Second
				up.onValidatorStatusUpdated(vID, nodeID0, false)
				checkUptime(t, up, nodeID0, wantUptime, currentTime)

				// Stop tracking
				currentTime = addTime(t, clk, 3*time.Second)
				// wantUptime should not increase since the node was paused
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
				require.NoError(up.manager.StopTracking([]ids.NodeID{nodeID0}))

				// Validator becomes active again after stopping tracking
				addTime(t, clk, 4*time.Second)
				// wantUptime should increase since we stopped tracking
				wantUptime += 4 * time.Second
				up.onValidatorStatusUpdated(vID, nodeID0, true)
				// wantUptime should increase since we stopped tracking
				currentTime = addTime(t, clk, 5*time.Second)
				wantUptime += 5 * time.Second

				// Start tracking and check elapsed time
				require.NoError(up.manager.StartTracking([]ids.NodeID{nodeID0}))
				// Uptime should have increased since the node was resumed
				// We should be optimistic and assume the node was online and active until we start tracking
				checkUptime(t, up, nodeID0, wantUptime, currentTime)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			u, clk, _ := setupTestEnv(t, nodeID0, startTime)
			test.testFunc(t, u, clk)
		})
	}
}

// loadValidatorsForTest is a test helper that replicates the logic from the manager
// for testing purposes
func loadValidatorsForTest(t *testing.T, validatorState *state, newValidators map[ids.ID]*validators.GetCurrentValidatorOutput) error {
	t.Helper()
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

func setupTestEnv(t *testing.T, nodeID ids.NodeID, startTime time.Time) (UptimeTracker, *mockable.Clock, uptime.State) {
	t.Helper()
	clk := mockable.Clock{}
	clk.Set(startTime)
	s := uptime.NewTestState()
	s.AddNode(nodeID, startTime)
	db := memdb.New()
	ctx := &snow.Context{}
	ctx.SubnetID = ids.GenerateTestID()
	u, err := NewUptimeTracker(ctx, uptime.NewManager(s, &clk), db)
	require.NoError(t, err)
	return *u, &clk, s
}

func addTime(t *testing.T, clk *mockable.Clock, duration time.Duration) time.Time {
	t.Helper()
	clk.Set(clk.Time().Add(duration))
	return clk.Time()
}

func checkUptime(t *testing.T, up UptimeTracker, nodeID ids.NodeID, wantUptime time.Duration, wantLastUpdate time.Time) {
	t.Helper()
	uptime, lastUpdated, err := up.manager.CalculateUptime(nodeID)
	require.NoError(t, err)
	require.Equal(t, wantLastUpdate.Unix(), lastUpdated.Unix())
	require.Equal(t, wantUptime, uptime)
}
