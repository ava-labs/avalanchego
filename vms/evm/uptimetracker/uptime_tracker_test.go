// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func TestUptimeTracker_EmptyInitialState(t *testing.T) {
	require := require.New(t)
	ut := setupUptimeTracker(t, nil, nil)

	// Initially no validators
	validatorList := ut.GetValidators()
	require.Empty(validatorList)
}

func TestUptimeTracker_AddSingleValidator(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()
	startTime := uint64(time.Now().Unix())

	// Add validator through Sync
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     startTime,
			IsActive:      true,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)

	require.NoError(ut.Sync(context.Background()))

	// Verify validator was added
	validatorList := ut.GetValidators()
	require.Len(validatorList, 1)
	require.Equal(nodeID, validatorList[0].NodeID)
	require.Equal(vID, validatorList[0].ValidationID)
	require.Equal(uint64(1000), validatorList[0].Weight)
	require.True(validatorList[0].IsActive)
	require.False(validatorList[0].IsL1Validator)

	// Verify GetValidator works
	validator, exists := ut.GetValidator(vID)
	require.True(exists)
	require.Equal(nodeID, validator.NodeID)
	require.Equal(vID, validator.ValidationID)
}

func TestUptimeTracker_RemoveValidator(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)

	require.NoError(ut.Sync(context.Background()))

	// Verify validator was added
	validatorList := ut.GetValidators()
	require.Len(validatorList, 1)

	// Remove validator by setting empty validator set
	ut = setupUptimeTracker(t, map[ids.ID]*validators.GetCurrentValidatorOutput{}, nil)
	require.NoError(ut.Sync(context.Background()))

	// Verify validator was removed
	validatorList = ut.GetValidators()
	require.Empty(validatorList)

	// Verify GetValidator returns false
	_, exists := ut.GetValidator(vID)
	require.False(exists)
}

func TestUptimeTracker_UpdateValidatorStatus(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add active validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)

	require.NoError(ut.Sync(context.Background()))

	// Verify validator is active
	validator, exists := ut.GetValidator(vID)
	require.True(exists)
	require.True(validator.IsActive)

	// Update validator to inactive
	validatorSet[vID].IsActive = false
	require.NoError(ut.Sync(context.Background()))

	// Verify validator is now inactive
	validator, exists = ut.GetValidator(vID)
	require.True(exists)
	require.False(validator.IsActive)
}

func TestUptimeTracker_UpdateValidatorWeight(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add validator with weight 1000
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)

	require.NoError(ut.Sync(context.Background()))

	// Verify initial weight
	validator, exists := ut.GetValidator(vID)
	require.True(exists)
	require.Equal(uint64(1000), validator.Weight)

	// Update weight
	validatorSet[vID].Weight = 2000
	require.NoError(ut.Sync(context.Background()))

	// Verify weight was updated
	validator, exists = ut.GetValidator(vID)
	require.True(exists)
	require.Equal(uint64(2000), validator.Weight)
}

func TestUptimeTracker_ImmutableFieldError(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)

	require.NoError(ut.Sync(context.Background()))

	// Try to change NodeID (immutable field)
	validatorSet[vID].NodeID = ids.GenerateTestNodeID()
	err := ut.Sync(context.Background())
	require.ErrorIs(err, ErrImmutableField)
}

func TestUptimeTracker_ValidatorLifecycle(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Test 1: Add active validator
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

	ut := setupUptimeTracker(t, firstSet, nil)

	// Initially no validators
	require.Empty(ut.GetValidators())

	// Connect node before it's a validator
	require.NoError(ut.Connect(nodeID))
	require.True(ut.IsConnected(nodeID))

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

	// Test 2: Validator becomes inactive
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
	ut2 := setupUptimeTracker(t, secondSet, nil)
	// Copy connection state
	ut2.connectedVdrs = ut.connectedVdrs
	require.NoError(ut2.Sync(context.Background()))

	// Validator should still be tracked but now inactive
	validatorList = ut2.GetValidators()
	require.Len(validatorList, 1)
	require.Equal(nodeID, validatorList[0].NodeID)
	require.Equal(vID, validatorList[0].ValidationID)
	require.True(ut2.IsConnected(nodeID)) // Still connected from our perspective

	// Get updated validator info
	validator, exists = ut2.GetValidator(vID)
	require.True(exists)
	require.False(validator.IsActive)

	// Test 3: Validator becomes active again
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
	ut3 := setupUptimeTracker(t, thirdSet, nil)
	// Copy connection state
	ut3.connectedVdrs = ut.connectedVdrs
	require.NoError(ut3.Sync(context.Background()))

	// Validator should still be tracked and active
	validatorList = ut3.GetValidators()
	require.Len(validatorList, 1)
	require.Equal(nodeID, validatorList[0].NodeID)
	require.Equal(vID, validatorList[0].ValidationID)
	require.True(ut3.IsConnected(nodeID))

	// Get updated validator info
	validator, exists = ut3.GetValidator(vID)
	require.True(exists)
	require.True(validator.IsActive)

	// Test 4: Validator removed
	ut4 := setupUptimeTracker(t, nil, nil)
	// Copy connection state
	ut4.connectedVdrs = ut.connectedVdrs
	require.NoError(ut4.Sync(context.Background()))

	// Validator should be removed
	validatorList = ut4.GetValidators()
	require.Empty(validatorList)
	require.True(ut4.IsConnected(nodeID)) // Still connected from our perspective
}

func TestUptimeTracker_ConnectBeforeValidator(t *testing.T) {
	require := require.New(t)
	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add inactive validator through Sync
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      false,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Connect before tracking
	require.NoError(ut.Connect(nodeID))
	addTime(t, ut.clock, time.Second)
	wantUptime := 1 * time.Second

	// Elapse Time
	addTime(t, ut.clock, time.Second)
	// Since we have not started tracking this node yet, its observed uptime should
	// be incremented even though it is actually paused.
	wantUptime += 1 * time.Second

	// Start tracking
	require.NoError(ut.manager.StartTracking([]ids.NodeID{nodeID}))
	// Uptime here should not increase after start tracking
	// since the node is still paused after we started tracking
	currentTime := addTime(t, ut.clock, time.Second)
	checkUptime(t, ut, nodeID, wantUptime, currentTime)

	// Disconnect
	require.NoError(ut.Disconnect(nodeID))
	// Uptime should not have increased
	checkUptime(t, ut, nodeID, wantUptime, currentTime)
}

func TestUptimeTracker_ValidatorBecomesInactive(t *testing.T) {
	require := require.New(t)
	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add active validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Start tracking
	require.NoError(ut.manager.StartTracking([]ids.NodeID{nodeID}))

	// Connect
	addTime(t, ut.clock, 1*time.Second)
	require.NoError(ut.Connect(nodeID))

	// Validator becomes inactive
	addTime(t, ut.clock, 1*time.Second)
	validatorSet[vID].IsActive = false
	require.NoError(ut.Sync(context.Background()))

	// Elapse time
	currentTime := addTime(t, ut.clock, 2*time.Second)
	// Uptime should be 1 second since the node was paused after 1 sec
	wantUptime := 1 * time.Second
	checkUptime(t, ut, nodeID, wantUptime, currentTime)

	// Disconnect and check uptime
	currentTime = addTime(t, ut.clock, 3*time.Second)
	require.NoError(ut.Disconnect(nodeID))
	// Uptime should not have increased since the node was paused
	checkUptime(t, ut, nodeID, wantUptime, currentTime)

	// Connect again and check uptime
	addTime(t, ut.clock, 4*time.Second)
	require.NoError(ut.Connect(nodeID))
	currentTime = addTime(t, ut.clock, 5*time.Second)
	// Uptime should not have increased since the node was paused
	checkUptime(t, ut, nodeID, wantUptime, currentTime)

	// Validator becomes active again
	currentTime = addTime(t, ut.clock, 6*time.Second)
	validatorSet[vID].IsActive = true
	require.NoError(ut.Sync(context.Background()))
	// Uptime should not have increased since the node was paused
	// and we just resumed it
	checkUptime(t, ut, nodeID, wantUptime, currentTime)

	// Elapsed time check
	currentTime = addTime(t, ut.clock, 7*time.Second)
	// Uptime should increase by 7 seconds above since the node was resumed
	wantUptime += 7 * time.Second
	checkUptime(t, ut, nodeID, wantUptime, currentTime)
}

func TestUptimeTracker_ValidatorInactiveBeforeTracking(t *testing.T) {
	require := require.New(t)
	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add inactive validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      false,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Start tracking
	addTime(t, ut.clock, time.Second)
	// Uptime should be 1 since the node was paused before we started tracking
	wantUptime := 1 * time.Second
	require.NoError(ut.manager.StartTracking([]ids.NodeID{nodeID}))

	// Connect and check uptime
	addTime(t, ut.clock, 1*time.Second)
	checkUptime(t, ut, nodeID, wantUptime, ut.clock.Time())
	require.NoError(ut.Connect(nodeID))

	currentTime := addTime(t, ut.clock, 2*time.Second)
	// Uptime should not have increased since the node was paused after we started tracking
	checkUptime(t, ut, nodeID, wantUptime, currentTime)

	// Disconnect and check uptime
	currentTime = addTime(t, ut.clock, 3*time.Second)
	require.NoError(ut.Disconnect(nodeID))
	// Uptime should not have increased since the node was paused
	checkUptime(t, ut, nodeID, wantUptime, currentTime)

	// Connect again and validator becomes active
	addTime(t, ut.clock, 4*time.Second)
	require.NoError(ut.Connect(nodeID))
	addTime(t, ut.clock, 5*time.Second)
	validatorSet[vID].IsActive = true
	require.NoError(ut.Sync(context.Background()))

	// Check uptime after resume
	currentTime = addTime(t, ut.clock, 6*time.Second)
	// Uptime should have increased by 6 seconds since the node was resumed
	wantUptime += 6 * time.Second
	checkUptime(t, ut, nodeID, wantUptime, currentTime)
}

func TestUptimeTracker_BasicUptimeTracking(t *testing.T) {
	require := require.New(t)
	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add active validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Start tracking and connect
	require.NoError(ut.manager.StartTracking([]ids.NodeID{nodeID}))
	addTime(t, ut.clock, time.Second)
	require.NoError(ut.Connect(nodeID))

	// Get initial uptime
	initialUptime, _, err := ut.manager.CalculateUptime(nodeID)
	require.NoError(err)

	// Validator becomes inactive
	addTime(t, ut.clock, 2*time.Second)
	validatorSet[vID].IsActive = false
	require.NoError(ut.Sync(context.Background()))

	// Wait some time and check that uptime doesn't increase while inactive
	addTime(t, ut.clock, 2*time.Second)
	_, lastUpdated, err := ut.manager.CalculateUptime(nodeID)
	require.NoError(err)
	require.Equal(lastUpdated.Unix(), ut.clock.Time().Unix())

	// Validator becomes active again
	validatorSet[vID].IsActive = true
	require.NoError(ut.Sync(context.Background()))

	// Connect and verify we can still track uptime
	require.NoError(ut.Connect(nodeID))
	addTime(t, ut.clock, 2*time.Second)
	finalUptime, _, err := ut.manager.CalculateUptime(nodeID)
	require.NoError(err)
	require.GreaterOrEqual(finalUptime, initialUptime) // Should have increased or stayed the same
}

func TestUptimeTracker_StopAndRestartTracking(t *testing.T) {
	require := require.New(t)
	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add active validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Start tracking and connect
	require.NoError(ut.manager.StartTracking([]ids.NodeID{nodeID}))
	addTime(t, ut.clock, time.Second)
	require.NoError(ut.Connect(nodeID))

	// Stop tracking
	currentTime := addTime(t, ut.clock, 2*time.Second)
	wantUptime := 2 * time.Second
	checkUptime(t, ut, nodeID, wantUptime, currentTime)
	require.NoError(ut.manager.StopTracking([]ids.NodeID{nodeID}))

	// Validator becomes inactive after stopping tracking
	addTime(t, ut.clock, 3*time.Second)
	// wantUptime should increase since we stopped tracking
	wantUptime += 3 * time.Second
	validatorSet[vID].IsActive = false
	require.NoError(ut.Sync(context.Background()))
	// wantUptime should increase since we stopped tracking (even if the node was paused)
	currentTime = addTime(t, ut.clock, 4*time.Second)
	wantUptime += 4 * time.Second

	// Start tracking and check elapsed time
	require.NoError(ut.manager.StartTracking([]ids.NodeID{nodeID}))
	// Uptime have increased since the node was paused before we started tracking
	// We should be optimistic and assume the node was online and active until we start tracking
	checkUptime(t, ut, nodeID, wantUptime, currentTime)
}

func TestUptimeTracker_ValidatorStatusChangesDuringStop(t *testing.T) {
	require := require.New(t)
	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Add active validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Start tracking and connect
	require.NoError(ut.manager.StartTracking([]ids.NodeID{nodeID}))
	addTime(t, ut.clock, time.Second)
	require.NoError(ut.Connect(nodeID))

	// Validator becomes inactive
	currentTime := addTime(t, ut.clock, 2*time.Second)
	// wantUptime should increase
	wantUptime := 2 * time.Second
	validatorSet[vID].IsActive = false
	require.NoError(ut.Sync(context.Background()))
	checkUptime(t, ut, nodeID, wantUptime, currentTime)

	// Stop tracking
	currentTime = addTime(t, ut.clock, 3*time.Second)
	// wantUptime should not increase since the node was paused
	checkUptime(t, ut, nodeID, wantUptime, currentTime)
	require.NoError(ut.manager.StopTracking([]ids.NodeID{nodeID}))

	// Validator becomes active again after stopping tracking
	addTime(t, ut.clock, 4*time.Second)
	// wantUptime should increase since we stopped tracking
	wantUptime += 4 * time.Second
	validatorSet[vID].IsActive = true
	require.NoError(ut.Sync(context.Background()))
	// wantUptime should increase since we stopped tracking
	currentTime = addTime(t, ut.clock, 5*time.Second)
	wantUptime += 5 * time.Second

	// Start tracking and check elapsed time
	require.NoError(ut.manager.StartTracking([]ids.NodeID{nodeID}))
	// Uptime should have increased since the node was resumed
	// We should be optimistic and assume the node was online and active until we start tracking
	checkUptime(t, ut, nodeID, wantUptime, currentTime)
}

//func TestUptimeTracker_GetUptimeNonExistent(t *testing.T) {
//	require := require.New(t)
//	ut := setupUptimeTracker(t, nil, nil)
//
//	nodeID := ids.GenerateTestNodeID()
//	_, _, err := ut.GetUptime(nodeID)
//	require.ErrorIs(err, database.ErrNotFound)
//}
//
//func TestUptimeTracker_SetUptimeNonExistent(t *testing.T) {
//	require := require.New(t)
//	ut := setupUptimeTracker(t, nil, nil)
//
//	nodeID := ids.GenerateTestNodeID()
//	startTime := time.Now()
//	err := ut.SetUptime(nodeID, 1, startTime)
//	require.ErrorIs(err, database.ErrNotFound)
//}
//
//func TestUptimeTracker_AddValidatorAndUptime(t *testing.T) {
//	require := require.New(t)
//
//	nodeID := ids.GenerateTestNodeID()
//	vID := ids.GenerateTestID()
//	startTime := time.Now()
//
//	// Add validator through Sync
//	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
//		vID: {
//			ValidationID:  vID,
//			NodeID:        nodeID,
//			Weight:        1,
//			StartTime:     uint64(startTime.Unix()),
//			IsActive:      true,
//			IsL1Validator: true,
//		},
//	}
//
//	ut := setupUptimeTracker(t, validatorSet, nil)
//	require.NoError(ut.Sync(context.Background()))
//
//	// Get uptime
//	uptime, lastUpdated, err := ut.GetUptime(nodeID)
//	require.NoError(err)
//	require.Equal(time.Duration(0), uptime)
//	require.Equal(startTime.Unix(), lastUpdated.Unix())
//
//	// Set uptime
//	newUptime := 2 * time.Minute
//	newLastUpdated := lastUpdated.Add(time.Hour)
//	require.NoError(ut.SetUptime(nodeID, newUptime, newLastUpdated))
//
//	// Get new uptime
//	uptime, lastUpdated, err = ut.GetUptime(nodeID)
//	require.NoError(err)
//	require.Equal(newUptime, uptime)
//	require.Equal(newLastUpdated, lastUpdated)
//}

func TestUptimeTracker_UpdateValidatorStatusThroughSync(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()
	startTime := time.Now()

	// Add active validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1,
			StartTime:     uint64(startTime.Unix()),
			IsActive:      true,
			IsL1Validator: true,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Verify validator is active
	validator, exists := ut.GetValidator(vID)
	require.True(exists)
	require.True(validator.IsActive)

	// Update validator to inactive
	validatorSet[vID].IsActive = false
	require.NoError(ut.Sync(context.Background()))

	// Verify validator is now inactive
	validator, exists = ut.GetValidator(vID)
	require.True(exists)
	require.False(validator.IsActive)
}

func TestUptimeTracker_UpdateValidatorWeightThroughSync(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()
	startTime := time.Now()

	// Add validator with weight 1
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1,
			StartTime:     uint64(startTime.Unix()),
			IsActive:      true,
			IsL1Validator: true,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Verify initial weight
	validator, exists := ut.GetValidator(vID)
	require.True(exists)
	require.Equal(uint64(1), validator.Weight)

	// Update weight
	validatorSet[vID].Weight = 2
	require.NoError(ut.Sync(context.Background()))

	// Verify weight was updated
	validator, exists = ut.GetValidator(vID)
	require.True(exists)
	require.Equal(uint64(2), validator.Weight)
}

func TestUptimeTracker_ImmutableFieldErrors(t *testing.T) {
	require := require.New(t)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()
	startTime := time.Now()

	// Add validator
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1,
			StartTime:     uint64(startTime.Unix()),
			IsActive:      true,
			IsL1Validator: true,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Try to change NodeID (immutable field)
	validatorSet[vID].NodeID = ids.GenerateTestNodeID()
	err := ut.Sync(context.Background())
	require.ErrorIs(err, ErrImmutableField)

	// Reset to original state
	validatorSet[vID].NodeID = nodeID
	require.NoError(ut.Sync(context.Background()))

	// Try to change StartTime (immutable field)
	validatorSet[vID].StartTime = uint64(startTime.Unix()) + 100
	err = ut.Sync(context.Background())
	require.ErrorIs(err, ErrImmutableField)

	// Reset to original state
	validatorSet[vID].StartTime = uint64(startTime.Unix())
	require.NoError(ut.Sync(context.Background()))

	// Try to change IsL1Validator (immutable field)
	validatorSet[vID].IsL1Validator = false
	err = ut.Sync(context.Background())
	require.ErrorIs(err, ErrImmutableField)
}

//func TestUptimeTracker_DeleteValidator(t *testing.T) {
//	require := require.New(t)
//
//	nodeID := ids.GenerateTestNodeID()
//	vID := ids.GenerateTestID()
//
//	// Add validator
//	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
//		vID: {
//			ValidationID:  vID,
//			NodeID:        nodeID,
//			Weight:        1,
//			StartTime:     0,
//			IsActive:      true,
//			IsL1Validator: true,
//		},
//	}
//
//	ut := setupUptimeTracker(t, validatorSet, nil)
//	require.NoError(ut.Sync(context.Background()))
//
//	// Verify validator exists
//	_, exists := ut.GetValidator(vID)
//	require.True(exists)
//
//	// Remove validator
//	ut = setupUptimeTracker(t, nil, nil)
//	require.NoError(ut.Sync(context.Background()))
//
//	// Verify validator was removed
//	_, exists = ut.GetValidator(vID)
//	require.False(exists)
//
//	// Get deleted uptime should fail
//	_, _, err := ut.GetUptime(nodeID)
//	require.ErrorIs(err, database.ErrNotFound)
//}
//
//func TestUptimeTracker_Persistence(t *testing.T) {
//	require := require.New(t)
//
//	nodeID := ids.GenerateTestNodeID()
//	vID := ids.GenerateTestID()
//	startTime := time.Now()
//
//	// Add validator through Sync
//	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
//		vID: {
//			ValidationID:  vID,
//			NodeID:        nodeID,
//			Weight:        1,
//			StartTime:     uint64(startTime.Unix()),
//			IsActive:      true,
//			IsL1Validator: true,
//		},
//	}
//
//	db := memdb.New()
//	ut := setupUptimeTracker(t, validatorSet, db)
//	require.NoError(ut.Sync(context.Background()))
//
//	// Verify validator was added
//	_, exists := ut.GetValidator(vID)
//	require.True(exists)
//
//	// Set uptime
//	newUptime := 2 * time.Minute
//	newLastUpdated := startTime.Add(time.Hour)
//	require.NoError(ut.SetUptime(nodeID, newUptime, newLastUpdated))
//
//	// Start tracking before shutdown to avoid "not started tracking" error
//	require.NoError(ut.manager.StartTracking([]ids.NodeID{nodeID}))
//
//	// Shutdown to persist state
//	require.NoError(ut.Shutdown())
//
//	// Create new UptimeTracker from same DB with same validator set
//	newUT := setupUptimeTracker(t, validatorSet, db)
//
//	// Verify validator was loaded from DB
//	validator, exists := newUT.GetValidator(vID)
//	require.True(exists)
//	require.Equal(nodeID, validator.NodeID)
//	require.Equal(vID, validator.ValidationID)
//
//	// Verify uptime was loaded from DB
//	uptime, lastUpdated, err := newUT.GetUptime(nodeID)
//	require.NoError(err)
//	require.Equal(newUptime, uptime)
//	require.Equal(newLastUpdated.Unix(), lastUpdated.Unix())
//}
//
func TestUptimeTracker_MultipleValidators(t *testing.T) {
	require := require.New(t)

	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	vID1 := ids.GenerateTestID()
	vID2 := ids.GenerateTestID()

	// Add multiple validators
	validatorSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID1: {
			ValidationID:  vID1,
			NodeID:        nodeID1,
			Weight:        1000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
		vID2: {
			ValidationID:  vID2,
			NodeID:        nodeID2,
			Weight:        2000,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: true,
		},
	}

	ut := setupUptimeTracker(t, validatorSet, nil)
	require.NoError(ut.Sync(context.Background()))

	// Verify both validators were added
	validatorList := ut.GetValidators()
	require.Len(validatorList, 2)

	// Verify first validator
	validator1, exists := ut.GetValidator(vID1)
	require.True(exists)
	require.Equal(nodeID1, validator1.NodeID)
	require.Equal(uint64(1000), validator1.Weight)
	require.False(validator1.IsL1Validator)

	// Verify second validator
	validator2, exists := ut.GetValidator(vID2)
	require.True(exists)
	require.Equal(nodeID2, validator2.NodeID)
	require.Equal(uint64(2000), validator2.Weight)
	require.True(validator2.IsL1Validator)

	// Update one validator
	validatorSet[vID1].Weight = 1500
	validatorSet[vID1].IsActive = false
	require.NoError(ut.Sync(context.Background()))

	// Verify update
	validator1, exists = ut.GetValidator(vID1)
	require.True(exists)
	require.Equal(uint64(1500), validator1.Weight)
	require.False(validator1.IsActive)

	// Remove one validator
	delete(validatorSet, vID1)
	require.NoError(ut.Sync(context.Background()))

	// Verify removal
	validatorList = ut.GetValidators()
	require.Len(validatorList, 1)
	require.Equal(vID2, validatorList[0].ValidationID)
}

// testValidatorState is a minimal implementation that only provides what we actually use
type testValidatorState struct {
	validators map[ids.ID]*validators.GetCurrentValidatorOutput
}

func (t *testValidatorState) GetCurrentValidatorSet(_ context.Context, _ ids.ID) (map[ids.ID]*validators.GetCurrentValidatorOutput, uint64, error) {
	return t.validators, uint64(len(t.validators)), nil
}

// Stub implementations for the rest of the interface (unused in our tests)
func (*testValidatorState) GetMinimumHeight(_ context.Context) (uint64, error) {
	return 0, nil
}

func (t *testValidatorState) GetCurrentHeight(_ context.Context) (uint64, error) {
	return uint64(len(t.validators)), nil
}

func (*testValidatorState) GetSubnetID(_ context.Context, _ ids.ID) (ids.ID, error) {
	return ids.Empty, nil
}

func (*testValidatorState) GetValidatorSet(_ context.Context, _ uint64, _ ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return nil, nil
}

// setupUptimeTracker is a helper function to setup a UptimeTracker for testing
// if validatorSet is nil, it will be initialized to an empty map.
// if db is nil, it will be initialized to a new memory database.
func setupUptimeTracker(t *testing.T, validatorSet map[ids.ID]*validators.GetCurrentValidatorOutput, db database.Database) *UptimeTracker {
	t.Helper()
	if validatorSet == nil {
		validatorSet = make(map[ids.ID]*validators.GetCurrentValidatorOutput)
	}
	if db == nil {
		db = memdb.New()
	}
	subnetID := ids.GenerateTestID()
	testVS := &testValidatorState{validators: validatorSet}
	validatorState := validators.NewLockedState(&sync.Mutex{}, testVS)

	ut, err := NewUptimeTracker(validatorState, subnetID, db)
	require.NoError(t, err)

	// Set the clock to a known time for consistent testing
	// This ensures that uptime calculations are based on a predictable time
	ut.clock.Set(time.Unix(0, 0)) // Start at epoch

	return ut
}

func addTime(t *testing.T, clk *mockable.Clock, duration time.Duration) time.Time {
	t.Helper()
	clk.Set(clk.Time().Add(duration))
	return clk.Time()
}

func checkUptime(t *testing.T, up *UptimeTracker, nodeID ids.NodeID, wantUptime time.Duration, wantLastUpdate time.Time) {
	t.Helper()
	uptime, lastUpdated, err := up.manager.CalculateUptime(nodeID)
	require.NoError(t, err)
	require.Equal(t, wantLastUpdate.Unix(), lastUpdated.Unix())
	require.Equal(t, wantUptime, uptime)
}
