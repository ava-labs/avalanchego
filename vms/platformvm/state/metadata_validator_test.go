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
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func TestValidatorUptimes(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()

	// get non-existent uptime
	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()
	_, _, err := state.GetUptime(nodeID, subnetID)
	require.ErrorIs(err, database.ErrNotFound)

	// set non-existent uptime
	err = state.SetUptime(nodeID, subnetID, 1, time.Now())
	require.ErrorIs(err, database.ErrNotFound)

	testMetadata := &validatorMetadata{
		UpDuration:  time.Hour,
		lastUpdated: time.Now(),
	}
	// load uptime
	state.LoadValidatorMetadata(nodeID, subnetID, testMetadata)

	// get uptime
	upDuration, lastUpdated, err := state.GetUptime(nodeID, subnetID)
	require.NoError(err)
	require.Equal(testMetadata.UpDuration, upDuration)
	require.Equal(testMetadata.lastUpdated, lastUpdated)

	// set uptime
	newUpDuration := testMetadata.UpDuration + 1
	newLastUpdated := testMetadata.lastUpdated.Add(time.Hour)
	require.NoError(state.SetUptime(nodeID, subnetID, newUpDuration, newLastUpdated))

	// get new uptime
	upDuration, lastUpdated, err = state.GetUptime(nodeID, subnetID)
	require.NoError(err)
	require.Equal(newUpDuration, upDuration)
	require.Equal(newLastUpdated, lastUpdated)

	// load uptime changes uptimes
	newTestMetadata := &validatorMetadata{
		UpDuration:  testMetadata.UpDuration + time.Hour,
		lastUpdated: testMetadata.lastUpdated.Add(time.Hour),
	}
	state.LoadValidatorMetadata(nodeID, subnetID, newTestMetadata)

	// get new uptime
	upDuration, lastUpdated, err = state.GetUptime(nodeID, subnetID)
	require.NoError(err)
	require.Equal(newTestMetadata.UpDuration, upDuration)
	require.Equal(newTestMetadata.lastUpdated, lastUpdated)

	// delete uptime
	state.DeleteValidatorMetadata(nodeID, subnetID)

	// get deleted uptime
	_, _, err = state.GetUptime(nodeID, subnetID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestWriteValidatorMetadata(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()

	primaryDB := memdb.New()
	subnetDB := memdb.New()

	// write empty uptimes
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))

	// load uptime
	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()
	testUptimeReward := &validatorMetadata{
		UpDuration:      time.Hour,
		lastUpdated:     time.Now(),
		PotentialReward: 100,
		txID:            ids.GenerateTestID(),
	}
	state.LoadValidatorMetadata(nodeID, subnetID, testUptimeReward)

	// write state, should not reflect to DB yet
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))
	require.False(primaryDB.Has(testUptimeReward.txID[:]))
	require.False(subnetDB.Has(testUptimeReward.txID[:]))

	// get uptime should still return the loaded value
	upDuration, lastUpdated, err := state.GetUptime(nodeID, subnetID)
	require.NoError(err)
	require.Equal(testUptimeReward.UpDuration, upDuration)
	require.Equal(testUptimeReward.lastUpdated, lastUpdated)

	// update uptimes
	newUpDuration := testUptimeReward.UpDuration + 1
	newLastUpdated := testUptimeReward.lastUpdated.Add(time.Hour)
	require.NoError(state.SetUptime(nodeID, subnetID, newUpDuration, newLastUpdated))

	// write uptimes, should reflect to subnet DB
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))
	require.False(primaryDB.Has(testUptimeReward.txID[:]))
	require.True(subnetDB.Has(testUptimeReward.txID[:]))
}

// func TestValidatorStakingInfo(t *testing.T) {
// 	state := newValidatorState()
//
// 	// get non-existent staking info
// 	nodeID := ids.GenerateTestNodeID()
// 	subnetID := ids.GenerateTestID()
// 	_, err := state.GetStakingInfo(subnetID, nodeID)
// 	require.ErrorIs(t, err, database.ErrNotFound)
//
// 	// set non-existent staking info
// 	err = state.SetStakingInfo(subnetID, nodeID, StakingInfo{DelegateeReward: 100000})
// 	require.ErrorIs(t, err, database.ErrNotFound)
//
// 	testMetadata := &validatorMetadata{
// 		PotentialDelegateeReward: 100000,
// 	}
// 	// load staking info
// 	state.LoadValidatorMetadata(nodeID, subnetID, testMetadata)
//
// 	// get staking info
// 	stakingInfo, err := state.GetStakingInfo(subnetID, nodeID)
// 	require.NoError(t, err)
// 	require.Equal(t, testMetadata.PotentialDelegateeReward, stakingInfo.DelegateeReward)
//
// 	// set staking info
// 	wantDelegateeReward := testMetadata.PotentialDelegateeReward + 100000
// 	require.NoError(t, state.SetStakingInfo(subnetID, nodeID, StakingInfo{DelegateeReward: wantDelegateeReward}))
//
// 	// get new staking info
// 	stakingInfo, err = state.GetStakingInfo(subnetID, nodeID)
// 	require.NoError(t, err)
// 	require.Equal(t, wantDelegateeReward, stakingInfo.DelegateeReward)
//
// 	// load staking info changes
// 	newTestMetadata := &validatorMetadata{
// 		PotentialDelegateeReward: testMetadata.PotentialDelegateeReward + 100000,
// 	}
// 	state.LoadValidatorMetadata(nodeID, subnetID, newTestMetadata)
//
// 	// get new staking info
// 	stakingInfo, err = state.GetStakingInfo(subnetID, nodeID)
// 	require.NoError(t, err)
// 	require.Equal(t, newTestMetadata.PotentialDelegateeReward, stakingInfo.DelegateeReward)
//
// 	// delete staking info
// 	state.DeleteValidatorMetadata(nodeID, subnetID)
//
// 	// get deleted staking info
// 	_, _, err = state.GetUptime(nodeID, subnetID)
// 	require.ErrorIs(t, err, database.ErrNotFound)
// }

func TestAddValidatorMetadataWrite(t *testing.T) {
	tests := []struct {
		name        string
		subnetID    ids.ID
		wantPrimary bool
	}{
		{
			name:        "primary network",
			subnetID:    constants.PrimaryNetworkID,
			wantPrimary: true,
		},
		{
			name:        "subnet",
			subnetID:    ids.GenerateTestID(),
			wantPrimary: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			state := newValidatorState()
			primaryDB := memdb.New()
			subnetDB := memdb.New()

			txID := ids.GenerateTestID()
			require.NoError(state.AddValidatorMetadata(ids.GenerateTestNodeID(), tt.subnetID,
				&validatorMetadata{
				txID:            txID,
				PotentialReward: 100,
				}))
			require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))

			hasPrimary, err := primaryDB.Has(txID[:])
			require.NoError(err)
			hasSubnet, err := subnetDB.Has(txID[:])
			require.NoError(err)

			require.Equal(tt.wantPrimary, hasPrimary)
			require.Equal(!tt.wantPrimary, hasSubnet)
		})
	}
}

func TestDeleteValidatorMetadataWrite(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()
	primaryDB := memdb.New()
	subnetDB := memdb.New()

	nodeID := ids.GenerateTestNodeID()
	txID := ids.GenerateTestID()
	require.NoError(state.AddValidatorMetadata(nodeID, constants.PrimaryNetworkID, &validatorMetadata{
		txID:            txID,
		PotentialReward: 100,
	}))
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))

	state.DeleteValidatorMetadata(nodeID, constants.PrimaryNetworkID)
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))

	has, err := primaryDB.Has(txID[:])
	require.NoError(err)
	require.False(has)
}

func TestAddThenDeleteValidatorMetadataWrite(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()
	primaryDB := memdb.New()
	subnetDB := memdb.New()

	nodeID := ids.GenerateTestNodeID()
	txID := ids.GenerateTestID()
	require.NoError(state.AddValidatorMetadata(nodeID, constants.PrimaryNetworkID, &validatorMetadata{
		txID:            txID,
		PotentialReward: 100,
	}))
	state.DeleteValidatorMetadata(nodeID, constants.PrimaryNetworkID)
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))

	has, err := primaryDB.Has(txID[:])
	require.NoError(err)
	require.False(has)
}

func TestDeleteThenReAddValidatorMetadataWrite(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()
	primaryDB := memdb.New()
	subnetDB := memdb.New()

	nodeID := ids.GenerateTestNodeID()
	oldTxID := ids.GenerateTestID()
	require.NoError(state.AddValidatorMetadata(nodeID, constants.PrimaryNetworkID, &validatorMetadata{
		txID:            oldTxID,
		PotentialReward: 100,
	}))
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))

	state.DeleteValidatorMetadata(nodeID, constants.PrimaryNetworkID)
	newTxID := ids.GenerateTestID()
	require.NoError(state.AddValidatorMetadata(nodeID, constants.PrimaryNetworkID, &validatorMetadata{
		txID:            newTxID,
		PotentialReward: 200,
	}))
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))

	has, err := primaryDB.Has(oldTxID[:])
	require.NoError(err)
	require.False(has)

	has, err = primaryDB.Has(newTxID[:])
	require.NoError(err)
	require.True(has)
}

func TestDeleteAddDeleteAddValidatorMetadataWrite(t *testing.T) {
	require := require.New(t)
	state := newValidatorState()
	primaryDB := memdb.New()
	subnetDB := memdb.New()

	nodeID := ids.GenerateTestNodeID()
	txID1 := ids.GenerateTestID()
	txID2 := ids.GenerateTestID()
	txID3 := ids.GenerateTestID()

	require.NoError(state.AddValidatorMetadata(nodeID, constants.PrimaryNetworkID, &validatorMetadata{
		txID:            txID1,
		PotentialReward: 100,
	}))
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))
	has, err := primaryDB.Has(txID1[:])
	require.NoError(err)
	require.True(has)

	state.DeleteValidatorMetadata(nodeID, constants.PrimaryNetworkID)
	require.NoError(state.AddValidatorMetadata(nodeID, constants.PrimaryNetworkID, &validatorMetadata{
		txID:            txID2,
		PotentialReward: 200,
	}))
	state.DeleteValidatorMetadata(nodeID, constants.PrimaryNetworkID)
	require.NoError(state.AddValidatorMetadata(nodeID, constants.PrimaryNetworkID, &validatorMetadata{
		txID:            txID3,
		PotentialReward: 300,
	}))
	require.NoError(state.WriteValidatorMetadata(primaryDB, subnetDB, CodecVersion1))

	has, err = primaryDB.Has(txID1[:])
	require.NoError(err)
	require.False(has)

	has, err = primaryDB.Has(txID2[:])
	require.NoError(err)
	require.False(has)

	has, err = primaryDB.Has(txID3[:])
	require.NoError(err)
	require.True(has)
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
