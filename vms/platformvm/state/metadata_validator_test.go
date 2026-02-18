// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestValidatorUptimes(t *testing.T) {
	require := require.New(t)
	state := newTestState(t, memdb.New())

	// get non-existent uptime
	staker := newTestStaker(constants.PrimaryNetworkID)

	_, _, err := state.GetUptime(staker.NodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// set non-existent uptime
	err = state.SetUptime(staker.NodeID, 1, time.Now())
	require.ErrorIs(err, database.ErrNotFound)

	wantUpDuration := time.Hour
	wantLastUpdated := time.Now()

	require.NoError(state.PutCurrentValidator(staker))
	require.NoError(state.Commit())

	require.NoError(state.SetUptime(staker.NodeID, wantUpDuration, wantLastUpdated))

	// get uptime
	upDuration, lastUpdated, err := state.GetUptime(staker.NodeID)
	require.NoError(err)
	require.Equal(wantUpDuration, upDuration)
	require.Equal(wantLastUpdated, lastUpdated)

	// set uptime
	wantUpDuration = wantUpDuration + 1
	wantLastUpdated = wantLastUpdated.Add(time.Hour)
	require.NoError(state.SetUptime(staker.NodeID, wantUpDuration, wantLastUpdated))

	// get new uptime
	upDuration, lastUpdated, err = state.GetUptime(staker.NodeID)
	require.NoError(err)
	require.Equal(wantUpDuration, upDuration)
	require.Equal(wantLastUpdated, lastUpdated)

	require.NoError(state.DeleteCurrentValidator(staker))
	require.NoError(state.Commit())

	// get deleted uptime
	_, _, err = state.GetUptime(staker.NodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestWriteValidatorMetadata(t *testing.T) {
	require := require.New(t)
	state := newTestState(t, memdb.New())

	staker := newTestStaker(constants.PrimaryNetworkID)

	require.NoError(state.PutCurrentValidator(staker))
	require.NoError(state.Commit())

	nodeID := staker.NodeID
	gotUptime, gotLastUpdated, err := state.GetUptime(nodeID)
	require.Zero(gotUptime)
	require.Equal(staker.StartTime, gotLastUpdated)
	require.NoError(err)

	// load uptime
	testUptimeReward := &validatorMetadata{
		UpDuration:      time.Hour,
		lastUpdated:     time.Now(),
		PotentialReward: 100,
		txID:            ids.GenerateTestID(),
	}

	require.NoError(state.SetUptime(nodeID, testUptimeReward.UpDuration, testUptimeReward.lastUpdated))

	gotUptime, gotLastUpdated, err = state.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(testUptimeReward.UpDuration, gotUptime)
	require.Equal(testUptimeReward.lastUpdated, gotLastUpdated)

	// update uptimes
	newUpDuration := testUptimeReward.UpDuration + 1
	newLastUpdated := testUptimeReward.lastUpdated.Add(time.Hour)
	require.NoError(state.SetUptime(nodeID, newUpDuration, newLastUpdated))

	gotUptime, gotLastUpdated, err = state.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(newUpDuration, gotUptime)
	require.Equal(newLastUpdated, gotLastUpdated)
}

func TestParseValidatorMetadata(t *testing.T) {
	type test struct {
		name        string
		bytes       []byte
		expected    *validatorMetadata
		expectedErr error
	}
	tests := []test{
		{
			name:  "nil",
			bytes: nil,
			expected: &validatorMetadata{
				lastUpdated: time.Unix(0, 0),
			},
			expectedErr: nil,
		},
		{
			name:  "nil",
			bytes: []byte{},
			expected: &validatorMetadata{
				lastUpdated: time.Unix(0, 0),
			},
			expectedErr: nil,
		},
		{
			name: "potential reward only",
			bytes: []byte{
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
			},
			expected: &validatorMetadata{
				PotentialReward: 100000,
				lastUpdated:     time.Unix(0, 0),
			},
			expectedErr: nil,
		},
		{
			name: "uptime + potential reward",
			bytes: []byte{
				// codec version
				0x00, 0x00,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
			},
			expected: &validatorMetadata{
				UpDuration:      6000000,
				LastUpdated:     900000,
				PotentialReward: 100000,
				lastUpdated:     time.Unix(900000, 0),
			},
			expectedErr: nil,
		},
		{
			name: "uptime + potential reward + potential delegatee reward",
			bytes: []byte{
				// codec version
				0x00, 0x00,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
				// potential delegatee reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4E, 0x20,
			},
			expected: &validatorMetadata{
				UpDuration:               6000000,
				LastUpdated:              900000,
				PotentialReward:          100000,
				PotentialDelegateeReward: 20000,
				lastUpdated:              time.Unix(900000, 0),
			},
			expectedErr: nil,
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
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
				// potential delegatee reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4E, 0x20,
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
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
				// potential delegatee reward
				0x00, 0x00, 0x00, 0x00, 0x4E, 0x20,
			},
			expected:    nil,
			expectedErr: wrappers.ErrInsufficientLength,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			var metadata validatorMetadata
			err := parseValidatorMetadata(tt.bytes, &metadata)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expected, &metadata)
		})
	}
}
