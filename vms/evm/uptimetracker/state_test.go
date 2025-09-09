// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorsmock"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func TestState(t *testing.T) {
	require := require.New(t)
	db := memdb.New()
	state, err := newState(db)
	require.NoError(err)

	// get non-existent uptime
	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()
	_, _, err = state.GetUptime(nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// set non-existent uptime
	startTime := time.Now()
	err = state.SetUptime(nodeID, 1, startTime)
	require.ErrorIs(err, database.ErrNotFound)

	// add new validator
	vdr := Validator{
		ValidationID:   vID,
		NodeID:         nodeID,
		Weight:         1,
		StartTimestamp: uint64(startTime.Unix()),
		IsActive:       true,
		IsL1Validator:  true,
	}
	require.NoError(state.addValidator(vdr))

	// adding the same validator should fail
	err = state.addValidator(vdr)
	require.ErrorIs(err, ErrAlreadyExists)
	// adding the same nodeID should fail
	newVdr := vdr
	newVdr.ValidationID = ids.GenerateTestID()
	err = state.addValidator(newVdr)
	require.ErrorIs(err, ErrAlreadyExists)

	// get uptime
	uptime, lastUpdated, err := state.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(time.Duration(0), uptime)
	require.Equal(startTime.Unix(), lastUpdated.Unix())

	// set uptime
	newUptime := 2 * time.Minute
	newLastUpdated := lastUpdated.Add(time.Hour)
	require.NoError(state.SetUptime(nodeID, newUptime, newLastUpdated))
	// get new uptime
	uptime, lastUpdated, err = state.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(newUptime, uptime)
	require.Equal(newLastUpdated, lastUpdated)

	// set status
	vdr.IsActive = false
	require.NoError(state.updateValidator(vdr))
	// get status
	vdata, ok := state.data[vID]
	require.True(ok)
	require.False(vdata.IsActive)

	// set weight
	newWeight := uint64(2)
	vdr.Weight = newWeight
	require.NoError(state.updateValidator(vdr))
	// get weight
	vdata, ok = state.data[vID]
	require.True(ok)
	require.Equal(newWeight, vdata.Weight)

	// set a different node ID should fail
	newNodeID := ids.GenerateTestNodeID()
	vdr.NodeID = newNodeID
	err = state.updateValidator(vdr)
	require.ErrorIs(err, ErrImmutableField)

	// set a different start time should fail
	vdr.StartTimestamp += 100
	err = state.updateValidator(vdr)
	require.ErrorIs(err, ErrImmutableField)

	// set IsL1Validator should fail
	vdr.IsL1Validator = false
	err = state.updateValidator(vdr)
	require.ErrorIs(err, ErrImmutableField)

	// set validation ID should result in not found
	vdr.ValidationID = ids.GenerateTestID()
	err = state.updateValidator(vdr)
	require.ErrorIs(err, database.ErrNotFound)

	// delete uptime
	require.True(state.deleteValidator(vID))

	// get deleted uptime
	_, _, err = state.GetUptime(nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestWriteValidator(t *testing.T) {
	require := require.New(t)
	db := memdb.New()
	state, err := newState(db)
	require.NoError(err)
	// write empty uptimes
	require.True(state.writeState())

	// load uptime
	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()
	startTime := time.Now()
	require.NoError(state.addValidator(Validator{
		ValidationID:   vID,
		NodeID:         nodeID,
		Weight:         1,
		StartTimestamp: uint64(startTime.Unix()),
		IsActive:       true,
		IsL1Validator:  true,
	}))

	// write state, should reflect to DB
	require.True(state.writeState())
	require.True(db.Has(vID[:]))

	// set uptime
	newUptime := 2 * time.Minute
	newLastUpdated := startTime.Add(time.Hour)
	require.NoError(state.SetUptime(nodeID, newUptime, newLastUpdated))
	require.True(state.writeState())

	// refresh state, should load from DB
	state, err = newState(db)
	require.NoError(err)

	// get uptime
	uptime, lastUpdated, err := state.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(newUptime, uptime)
	require.Equal(newLastUpdated.Unix(), lastUpdated.Unix())

	// delete
	require.True(state.deleteValidator(vID))

	// write state, should reflect to DB
	require.True(state.writeState())
	require.False(db.Has(vID[:]))
}

func TestParseValidator(t *testing.T) {
	testNodeID, err := ids.NodeIDFromString("NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat")
	require.NoError(t, err)
	type test struct {
		name        string
		bytes       []byte
		expected    *validatorData
		expectedErr error
	}
	tests := []test{
		{
			name:  "nil",
			bytes: nil,
			expected: &validatorData{
				LastUpdated:   0,
				StartTime:     0,
				validationID:  ids.Empty,
				NodeID:        ids.EmptyNodeID,
				UpDuration:    0,
				Weight:        0,
				IsActive:      false,
				IsL1Validator: false,
			},
			expectedErr: nil,
		},
		{
			name:  "empty",
			bytes: []byte{},
			expected: &validatorData{
				LastUpdated:   0,
				StartTime:     0,
				validationID:  ids.Empty,
				NodeID:        ids.EmptyNodeID,
				UpDuration:    0,
				Weight:        0,
				IsActive:      false,
				IsL1Validator: false,
			},
			expectedErr: nil,
		},
		{
			name: "valid",
			bytes: []byte{
				// codec version
				0x00, 0x00,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// node ID
				0x7e, 0xef, 0xe8, 0x8a, 0x45, 0xfb, 0x7a, 0xc4,
				0xb0, 0x59, 0xc9, 0x33, 0x71, 0x0a, 0x57, 0x33,
				0xff, 0x9f, 0x4b, 0xab,
				// weight
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				// start time
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// status
				0x01,
				// IsL1Validator
				0x01,
			},
			expected: &validatorData{
				UpDuration:    time.Duration(6000000),
				LastUpdated:   900000,
				NodeID:        testNodeID,
				StartTime:     6000000,
				IsActive:      true,
				Weight:        1,
				IsL1Validator: true,
			},
		},
		{
			name: "invalid codec version",
			bytes: []byte{
				// codec version
				0x00, 0x02,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
			},
			expected:    nil,
			expectedErr: codec.ErrUnknownVersion,
		},
		{
			name: "short byte len",
			bytes: []byte{
				// codec version
				0x00, 0x00,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
			},
			expected:    nil,
			expectedErr: wrappers.ErrInsufficientLength,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			var data validatorData
			err := parseValidatorData(tt.bytes, &data)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expected, &data)
		})
	}
}

func TestStateModifications(t *testing.T) {
	require := require.New(t)
	db := memdb.New()
	state, err := newState(db)
	require.NoError(err)

	expectedvID := ids.GenerateTestID()
	expectedNodeID := ids.GenerateTestNodeID()
	expectedStartTime := time.Now()

	// add initial validator to test state functionality
	initialvID := ids.GenerateTestID()
	initialNodeID := ids.GenerateTestNodeID()
	initialStartTime := time.Now()

	// add initial validator
	require.NoError(state.addValidator(Validator{
		ValidationID:   initialvID,
		NodeID:         initialNodeID,
		Weight:         1,
		StartTimestamp: uint64(initialStartTime.Unix()),
		IsActive:       true,
		IsL1Validator:  true,
	}))

	// Verify initial validator was added
	vdata, ok := state.data[initialvID]
	require.True(ok)
	require.Equal(initialNodeID, vdata.NodeID)
	require.Equal(uint64(1), vdata.Weight)
	require.Equal(uint64(initialStartTime.Unix()), vdata.StartTime)
	require.True(vdata.IsActive)
	require.True(vdata.IsL1Validator)

	// add new validator
	vdr := Validator{
		ValidationID:   expectedvID,
		NodeID:         expectedNodeID,
		Weight:         1,
		StartTimestamp: uint64(expectedStartTime.Unix()),
		IsActive:       true,
		IsL1Validator:  true,
	}
	require.NoError(state.addValidator(vdr))

	// Verify new validator was added
	vdata, ok = state.data[expectedvID]
	require.True(ok)
	require.Equal(expectedNodeID, vdata.NodeID)
	require.Equal(uint64(1), vdata.Weight)
	require.Equal(uint64(expectedStartTime.Unix()), vdata.StartTime)
	require.True(vdata.IsActive)
	require.True(vdata.IsL1Validator)

	// update validator status
	vdr.IsActive = false
	require.NoError(state.updateValidator(vdr))

	// Verify status was updated
	vdata, ok = state.data[expectedvID]
	require.True(ok)
	require.False(vdata.IsActive)

	// remove validator
	require.True(state.deleteValidator(expectedvID))

	// Verify validator was removed
	_, ok = state.data[expectedvID]
	require.False(ok)
}

func TestSync_TriggersPauseResumeEventsViaChainCtx(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// Prepare mock validator state to drive two successive Sync calls
	mockVS := validatorsmock.NewState(ctrl)
	ctx := &snow.Context{}
	ctx.SubnetID = ids.GenerateTestID()
	ctx.ValidatorState = validators.NewLockedState(&ctx.Lock, mockVS)

	// Build NewUptimeTracker with real state and uptime manager
	db := memdb.New()
	clk := mockable.Clock{}
	clk.Set(time.Unix(0, 0))
	ut, err := NewUptimeTracker(ctx, db, &clk)
	require.NoError(err)

	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()

	// Mark node as connected at the tracker before syncs
	require.NoError(ut.Connect(nodeID))

	// First sync: node active -> should be added and connected (not paused)
	firstSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}
	mockVS.EXPECT().GetCurrentValidatorSet(gomock.Any(), ctx.SubnetID).Return(firstSet, uint64(1), nil)
	require.NoError(ut.Sync(context.Background()))
	require.False(ut.pausableManager.isPaused(nodeID))
	require.True(ut.pausableManager.manager.IsConnected(nodeID))

	// Second sync: same validator toggles to inactive -> should pause and disconnect
	secondSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1,
			StartTime:     0,
			IsActive:      false,
			IsL1Validator: false,
		},
	}
	mockVS.EXPECT().GetCurrentValidatorSet(gomock.Any(), ctx.SubnetID).Return(secondSet, uint64(2), nil)
	require.NoError(ut.Sync(context.Background()))
	require.True(ut.pausableManager.isPaused(nodeID))
	require.False(ut.pausableManager.manager.IsConnected(nodeID))

	// Third sync: back to active -> should resume and connect (since previously connected)
	thirdSet := map[ids.ID]*validators.GetCurrentValidatorOutput{
		vID: {
			ValidationID:  vID,
			NodeID:        nodeID,
			Weight:        1,
			StartTime:     0,
			IsActive:      true,
			IsL1Validator: false,
		},
	}
	mockVS.EXPECT().GetCurrentValidatorSet(gomock.Any(), ctx.SubnetID).Return(thirdSet, uint64(3), nil)
	require.NoError(ut.Sync(context.Background()))
	require.False(ut.pausableManager.isPaused(nodeID))
	require.True(ut.pausableManager.manager.IsConnected(nodeID))
}
