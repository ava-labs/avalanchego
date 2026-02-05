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
)

func TestWriteValidatorMetadata(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()

	primaryDB := memdb.New()
	subnetDB := memdb.New()

	// write no metadata
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))

	// load metadata
	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()
	vdrMetadata := &validatorMetadata{
		UpDuration:      time.Hour,
		PotentialReward: 100,
		txID:            ids.GenerateTestID(),
	}
	state.LoadValidatorMetadata(nodeID, subnetID, vdrMetadata)

	// write state, should not reflect to DB yet
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))
	require.False(primaryDB.Has(vdrMetadata.txID[:]))
	require.False(subnetDB.Has(vdrMetadata.txID[:]))

	// get delegatee reward should return the loaded value
	delegateeReward, err := state.GetDelegateeReward(subnetID, nodeID)
	require.NoError(err)
	require.Equal(vdrMetadata.PotentialDelegateeReward, delegateeReward)

	// update delegatee reward
	newDelegateeReward := vdrMetadata.PotentialDelegateeReward + 100
	require.NoError(state.SetDelegateeReward(subnetID, nodeID, newDelegateeReward))

	// write metadata, should reflect to subnet DB
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))
	require.False(primaryDB.Has(vdrMetadata.txID[:]))
	require.True(subnetDB.Has(vdrMetadata.txID[:]))
}

func TestValidatorDelegateeRewards(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()

	// get non-existent delegatee reward
	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()
	_, err := state.GetDelegateeReward(subnetID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// set non-existent delegatee reward
	err = state.SetDelegateeReward(subnetID, nodeID, 100000)
	require.ErrorIs(err, database.ErrNotFound)

	testMetadata := &validatorMetadata{
		PotentialDelegateeReward: 100000,
	}
	// load delegatee reward
	state.LoadValidatorMetadata(nodeID, subnetID, testMetadata)

	// get delegatee reward
	delegateeReward, err := state.GetDelegateeReward(subnetID, nodeID)
	require.NoError(err)
	require.Equal(testMetadata.PotentialDelegateeReward, delegateeReward)

	// set delegatee reward
	newDelegateeReward := testMetadata.PotentialDelegateeReward + 100000
	require.NoError(state.SetDelegateeReward(subnetID, nodeID, newDelegateeReward))

	// get new delegatee reward
	delegateeReward, err = state.GetDelegateeReward(subnetID, nodeID)
	require.NoError(err)
	require.Equal(newDelegateeReward, delegateeReward)

	// load delegatee reward changes
	newTestMetadata := &validatorMetadata{
		PotentialDelegateeReward: testMetadata.PotentialDelegateeReward + 100000,
	}
	state.LoadValidatorMetadata(nodeID, subnetID, newTestMetadata)

	// get new delegatee reward
	delegateeReward, err = state.GetDelegateeReward(subnetID, nodeID)
	require.NoError(err)
	require.Equal(newTestMetadata.PotentialDelegateeReward, delegateeReward)

	// delete delegatee reward
	state.DeleteValidatorMetadata(nodeID, subnetID)

	// get deleted delegatee reward
	_, err = state.GetDelegateeReward(subnetID, nodeID)
	require.ErrorIs(err, database.ErrNotFound)
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
			name:        "nil",
			bytes:       nil,
			expected:    &validatorMetadata{},
			expectedErr: nil,
		},
		{
			name:        "nil",
			bytes:       []byte{},
			expected:    &validatorMetadata{},
			expectedErr: nil,
		},
		{
			name: "potential reward only",
			bytes: []byte{
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
			},
			expected: &validatorMetadata{
				PotentialReward: 100000,
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
