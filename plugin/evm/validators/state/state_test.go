// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/subnet-evm/plugin/evm/validators/state/interfaces"
)

func TestState(t *testing.T) {
	require := require.New(t)
	db := memdb.New()
	state, err := NewState(db)
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
	vdr := interfaces.Validator{
		ValidationID:   vID,
		NodeID:         nodeID,
		Weight:         1,
		StartTimestamp: uint64(startTime.Unix()),
		IsActive:       true,
		IsL1Validator:  true,
	}
	state.AddValidator(vdr)

	// adding the same validator should fail
	err = state.AddValidator(vdr)
	require.ErrorIs(err, ErrAlreadyExists)
	// adding the same nodeID should fail
	newVdr := vdr
	newVdr.ValidationID = ids.GenerateTestID()
	err = state.AddValidator(newVdr)
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
	require.NoError(state.UpdateValidator(vdr))
	// get status
	data, err := state.GetValidator(vID)
	require.NoError(err)
	require.False(data.IsActive)

	// set weight
	newWeight := uint64(2)
	vdr.Weight = newWeight
	require.NoError(state.UpdateValidator(vdr))
	// get weight
	data, err = state.GetValidator(vID)
	require.NoError(err)
	require.Equal(newWeight, data.Weight)

	// set a different node ID should fail
	newNodeID := ids.GenerateTestNodeID()
	vdr.NodeID = newNodeID
	err = state.UpdateValidator(vdr)
	require.ErrorIs(err, ErrImmutableField)

	// set a different start time should fail
	vdr.StartTimestamp = vdr.StartTimestamp + 100
	err = state.UpdateValidator(vdr)
	require.ErrorIs(err, ErrImmutableField)

	// set IsL1Validator should fail
	vdr.IsL1Validator = false
	err = state.UpdateValidator(vdr)
	require.ErrorIs(err, ErrImmutableField)

	// set validation ID should result in not found
	vdr.ValidationID = ids.GenerateTestID()
	err = state.UpdateValidator(vdr)
	require.ErrorIs(err, database.ErrNotFound)

	// delete uptime
	require.NoError(state.DeleteValidator(vID))

	// get deleted uptime
	_, _, err = state.GetUptime(nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestWriteValidator(t *testing.T) {
	require := require.New(t)
	db := memdb.New()
	state, err := NewState(db)
	require.NoError(err)
	// write empty uptimes
	require.NoError(state.WriteState())

	// load uptime
	nodeID := ids.GenerateTestNodeID()
	vID := ids.GenerateTestID()
	startTime := time.Now()
	require.NoError(state.AddValidator(interfaces.Validator{
		ValidationID:   vID,
		NodeID:         nodeID,
		Weight:         1,
		StartTimestamp: uint64(startTime.Unix()),
		IsActive:       true,
		IsL1Validator:  true,
	}))

	// write state, should reflect to DB
	require.NoError(state.WriteState())
	require.True(db.Has(vID[:]))

	// set uptime
	newUptime := 2 * time.Minute
	newLastUpdated := startTime.Add(time.Hour)
	require.NoError(state.SetUptime(nodeID, newUptime, newLastUpdated))
	require.NoError(state.WriteState())

	// refresh state, should load from DB
	state, err = NewState(db)
	require.NoError(err)

	// get uptime
	uptime, lastUpdated, err := state.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(newUptime, uptime)
	require.Equal(newLastUpdated.Unix(), lastUpdated.Unix())

	// delete
	require.NoError(state.DeleteValidator(vID))

	// write state, should reflect to DB
	require.NoError(state.WriteState())
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

func TestStateListener(t *testing.T) {
	require := require.New(t)
	db := memdb.New()
	state, err := NewState(db)
	require.NoError(err)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	expectedvID := ids.GenerateTestID()
	expectedNodeID := ids.GenerateTestNodeID()
	expectedStartTime := time.Now()
	mockListener := interfaces.NewMockStateCallbackListener(ctrl)
	// add initial validator to test RegisterListener
	initialvID := ids.GenerateTestID()
	initialNodeID := ids.GenerateTestNodeID()
	initialStartTime := time.Now()

	// add initial validator
	require.NoError(state.AddValidator(interfaces.Validator{
		ValidationID:   initialvID,
		NodeID:         initialNodeID,
		Weight:         1,
		StartTimestamp: uint64(initialStartTime.Unix()),
		IsActive:       true,
		IsL1Validator:  true,
	}))

	// register listener
	mockListener.EXPECT().OnValidatorAdded(initialvID, initialNodeID, uint64(initialStartTime.Unix()), true)
	state.RegisterListener(mockListener)

	// add new validator
	mockListener.EXPECT().OnValidatorAdded(expectedvID, expectedNodeID, uint64(expectedStartTime.Unix()), true)
	vdr := interfaces.Validator{
		ValidationID:   expectedvID,
		NodeID:         expectedNodeID,
		Weight:         1,
		StartTimestamp: uint64(expectedStartTime.Unix()),
		IsActive:       true,
		IsL1Validator:  true,
	}
	require.NoError(state.AddValidator(vdr))

	// set status
	mockListener.EXPECT().OnValidatorStatusUpdated(expectedvID, expectedNodeID, false)
	vdr.IsActive = false
	require.NoError(state.UpdateValidator(vdr))

	// set status twice should not trigger listener
	require.NoError(state.UpdateValidator(vdr))

	// remove validator
	mockListener.EXPECT().OnValidatorRemoved(expectedvID, expectedNodeID)
	require.NoError(state.DeleteValidator(expectedvID))
}
