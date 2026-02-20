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

func newTestValidatorState() *validatorState {
	return newValidatorState(memdb.New(), memdb.New())
}

func newTestValidatorMetadata(txID ids.ID, upDuration time.Duration, lastUpdated time.Time, delegateeReward uint64) *validatorMetadata {
	return &validatorMetadata{
		txID:                     txID,
		UpDuration:               upDuration,
		LastUpdated:              uint64(lastUpdated.Unix()),
		lastUpdated:              lastUpdated,
		PotentialDelegateeReward: delegateeReward,
	}
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
			name:  "empty slice",
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

func TestValidatorState_Uptime(t *testing.T) {
	require := require.New(t)
	state := newTestValidatorState()

	nodeID := ids.GenerateTestNodeID()
	txID := ids.GenerateTestID()
	initialUpDuration := time.Hour
	initialLastUpdated := time.Unix(1000, 0)

	// GetUptime on non-existent validator returns ErrNotFound
	_, _, err := state.GetUptime(nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// SetUptime on non-existent validator returns ErrNotFound
	err = state.SetUptime(nodeID, time.Hour, time.Unix(2000, 0))
	require.ErrorIs(err, database.ErrNotFound)

	// Load validator metadata for Primary Network
	metadata := newTestValidatorMetadata(txID, initialUpDuration, initialLastUpdated, 0)
	require.NoError(state.LoadValidatorMetadata(nodeID, constants.PrimaryNetworkID, metadata))

	// GetUptime returns loaded values
	upDuration, lastUpdated, err := state.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(initialUpDuration, upDuration)
	require.Equal(initialLastUpdated, lastUpdated)

	// SetUptime updates the uptime
	newUpDuration := 2 * time.Hour
	newLastUpdated := initialLastUpdated.Add(time.Hour)
	require.NoError(state.SetUptime(nodeID, newUpDuration, newLastUpdated))

	// GetUptime returns updated (dirty) values
	upDuration, lastUpdated, err = state.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(newUpDuration, upDuration)
	require.Equal(newLastUpdated, lastUpdated)
}

// Test GetValidatorMutables and SetValidatorMutables
func TestValidatorState_Mutables(t *testing.T) {
	require := require.New(t)
	state := newTestValidatorState()

	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()
	txID := ids.GenerateTestID()
	initialDelegateeReward := uint64(1000)

	// GetValidatorMutables on non-existent validator returns ErrNotFound
	_, err := state.GetValidatorMutables(nodeID, subnetID)
	require.ErrorIs(err, database.ErrNotFound)

	// SetValidatorMutables on non-existent validator returns ErrNotFound
	err = state.SetValidatorMutables(nodeID, subnetID, ValidatorMutables{DelegateeReward: 100})
	require.ErrorIs(err, database.ErrNotFound)

	// Load validator metadata
	metadata := newTestValidatorMetadata(txID, 0, time.Time{}, initialDelegateeReward)
	require.NoError(state.LoadValidatorMetadata(nodeID, subnetID, metadata))

	// GetValidatorMutables returns loaded values
	mutables, err := state.GetValidatorMutables(nodeID, subnetID)
	require.NoError(err)
	require.Equal(initialDelegateeReward, mutables.DelegateeReward)

	// SetValidatorMutables updates the mutables
	newDelegateeReward := uint64(2000)
	require.NoError(state.SetValidatorMutables(nodeID, subnetID, ValidatorMutables{DelegateeReward: newDelegateeReward}))

	// GetValidatorMutables returns updated (dirty) values
	mutables, err = state.GetValidatorMutables(nodeID, subnetID)
	require.NoError(err)
	require.Equal(newDelegateeReward, mutables.DelegateeReward)
}

// Test that validators on different subnets are independent
func TestValidatorState_MultipleSubnets(t *testing.T) {
	require := require.New(t)
	state := newTestValidatorState()

	nodeID := ids.GenerateTestNodeID()
	subnet1 := ids.GenerateTestID()
	subnet2 := ids.GenerateTestID()

	// Load same validator on two different subnets
	metadata1 := newTestValidatorMetadata(ids.GenerateTestID(), 0, time.Time{}, 1000)
	metadata2 := newTestValidatorMetadata(ids.GenerateTestID(), 0, time.Time{}, 2000)
	require.NoError(state.LoadValidatorMetadata(nodeID, subnet1, metadata1))
	require.NoError(state.LoadValidatorMetadata(nodeID, subnet2, metadata2))

	// Each subnet has independent values
	mutables1, err := state.GetValidatorMutables(nodeID, subnet1)
	require.NoError(err)
	require.Equal(uint64(1000), mutables1.DelegateeReward)

	mutables2, err := state.GetValidatorMutables(nodeID, subnet2)
	require.NoError(err)
	require.Equal(uint64(2000), mutables2.DelegateeReward)

	// Updating one subnet doesn't affect the other
	require.NoError(state.SetValidatorMutables(nodeID, subnet1, ValidatorMutables{DelegateeReward: 5000}))

	mutables1, err = state.GetValidatorMutables(nodeID, subnet1)
	require.NoError(err)
	require.Equal(uint64(5000), mutables1.DelegateeReward)

	mutables2, err = state.GetValidatorMutables(nodeID, subnet2)
	require.NoError(err)
	require.Equal(uint64(2000), mutables2.DelegateeReward) // unchanged
}

// Test that multiple validators are independent
func TestValidatorState_MultipleValidators(t *testing.T) {
	require := require.New(t)
	state := newTestValidatorState()

	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()
	subnetID := constants.PrimaryNetworkID
	now := time.Unix(1000, 0)

	// Load two validators
	metadata1 := newTestValidatorMetadata(ids.GenerateTestID(), time.Hour, now, 1000)
	metadata2 := newTestValidatorMetadata(ids.GenerateTestID(), 2*time.Hour, now, 2000)
	require.NoError(state.LoadValidatorMetadata(nodeID1, subnetID, metadata1))
	require.NoError(state.LoadValidatorMetadata(nodeID2, subnetID, metadata2))

	// Each validator has independent values
	upDuration1, _, err := state.GetUptime(nodeID1)
	require.NoError(err)
	require.Equal(time.Hour, upDuration1)

	upDuration2, _, err := state.GetUptime(nodeID2)
	require.NoError(err)
	require.Equal(2*time.Hour, upDuration2)

	// Updating one validator doesn't affect the other
	require.NoError(state.SetUptime(nodeID1, 10*time.Hour, now))

	upDuration1, _, err = state.GetUptime(nodeID1)
	require.NoError(err)
	require.Equal(10*time.Hour, upDuration1)

	upDuration2, _, err = state.GetUptime(nodeID2)
	require.NoError(err)
	require.Equal(2*time.Hour, upDuration2) // unchanged
}

// Test DeleteValidatorMetadata
func TestValidatorState_Delete(t *testing.T) {
	require := require.New(t)
	state := newTestValidatorState()

	nodeID := ids.GenerateTestNodeID()
	subnetID := constants.PrimaryNetworkID
	now := time.Unix(1000, 0)

	// Delete non-existent validator returns ErrNotFound
	err := state.DeleteValidatorMetadata(nodeID, subnetID)
	require.ErrorIs(err, database.ErrNotFound)

	// Load and then delete
	metadata := newTestValidatorMetadata(ids.GenerateTestID(), time.Hour, now, 1000)
	require.NoError(state.LoadValidatorMetadata(nodeID, subnetID, metadata))

	// Set some dirty values
	require.NoError(state.SetUptime(nodeID, 2*time.Hour, now))
	require.NoError(state.SetValidatorMutables(nodeID, subnetID, ValidatorMutables{DelegateeReward: 5000}))

	// Delete clears dirty values
	require.NoError(state.DeleteValidatorMetadata(nodeID, subnetID))

	_, _, err = state.GetUptime(nodeID)
	require.ErrorIs(err, database.ErrNotFound)
	_, err = state.GetValidatorMutables(nodeID, subnetID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Test that LoadValidatorMetadata fails with uncommitted uptime
func TestValidatorState_LoadWithUncommittedUptime(t *testing.T) {
	require := require.New(t)
	state := newTestValidatorState()

	nodeID := ids.GenerateTestNodeID()
	subnetID := constants.PrimaryNetworkID
	now := time.Unix(1000, 0)

	// Load initial metadata
	metadata := newTestValidatorMetadata(ids.GenerateTestID(), time.Hour, now, 1000)
	require.NoError(state.LoadValidatorMetadata(nodeID, subnetID, metadata))

	// Set dirty uptime
	require.NoError(state.SetUptime(nodeID, 2*time.Hour, now))

	// Attempting to load again should fail due to uncommitted uptime
	newMetadata := newTestValidatorMetadata(ids.GenerateTestID(), 3*time.Hour, now, 2000)
	err := state.LoadValidatorMetadata(nodeID, subnetID, newMetadata)
	require.ErrorIs(err, errUncommittedChanges)
}

// Test that LoadValidatorMetadata fails with uncommitted mutables changes
func TestValidatorState_LoadWithUncommittedMutables(t *testing.T) {
	require := require.New(t)
	state := newTestValidatorState()

	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID() // non-primary network

	// Load initial metadata
	metadata := newTestValidatorMetadata(ids.GenerateTestID(), 0, time.Time{}, 1000)
	require.NoError(state.LoadValidatorMetadata(nodeID, subnetID, metadata))

	// Set dirty mutables
	require.NoError(state.SetValidatorMutables(nodeID, subnetID, ValidatorMutables{DelegateeReward: 5000}))

	// Attempting to load again should fail due to uncommitted changes
	newMetadata := newTestValidatorMetadata(ids.GenerateTestID(), 0, time.Time{}, 2000)
	err := state.LoadValidatorMetadata(nodeID, subnetID, newMetadata)
	require.ErrorIs(err, errUncommittedChanges)
}

// Test Commit persists changes to database
func TestValidatorState_Write(t *testing.T) {
	require := require.New(t)

	primaryDB := memdb.New()
	subnetDB := memdb.New()
	state := newValidatorState(primaryDB, subnetDB)

	nodeID := ids.GenerateTestNodeID()
	txID := ids.GenerateTestID()
	now := time.Unix(1000, 0)

	// Load validator on Primary Network
	metadata := newTestValidatorMetadata(txID, time.Hour, now, 1000)
	require.NoError(state.LoadValidatorMetadata(nodeID, constants.PrimaryNetworkID, metadata))

	// No writes yet - database should be empty
	_, err := primaryDB.Get(txID[:])
	require.ErrorIs(err, database.ErrNotFound)

	// Set dirty values
	require.NoError(state.SetUptime(nodeID, 2*time.Hour, now.Add(time.Hour)))
	require.NoError(state.SetValidatorMutables(nodeID, constants.PrimaryNetworkID, ValidatorMutables{DelegateeReward: 5000}))

	// Write to database
	require.NoError(state.Commit(CodecVersion1))

	// Database should now have the entry
	_, err = primaryDB.Get(txID[:])
	require.NoError(err)

	// After write, can load new metadata without error (dirty state cleared)
	newMetadata := newTestValidatorMetadata(txID, 3*time.Hour, now, 9000)
	require.NoError(state.LoadValidatorMetadata(nodeID, constants.PrimaryNetworkID, newMetadata))

	require.NoError(state.DeleteValidatorMetadata(nodeID, constants.PrimaryNetworkID))
	// Write to database
	require.NoError(state.Commit(CodecVersion1))

	// Deleted from db, should be empty.
	_, err = primaryDB.Get(txID[:])
	require.ErrorIs(err, database.ErrNotFound)

	_, _, err = state.GetUptime(nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	_, err = state.GetValidatorMutables(nodeID, constants.PrimaryNetworkID)
	require.ErrorIs(err, database.ErrNotFound)
}

// Test Commit writes to correct database based on subnet
func TestValidatorState_WriteToCorrectDB(t *testing.T) {
	require := require.New(t)

	primaryDB := memdb.New()
	subnetDB := memdb.New()
	state := newValidatorState(primaryDB, subnetDB)

	nodeID := ids.GenerateTestNodeID()
	primaryTxID := ids.GenerateTestID()
	subnetTxID := ids.GenerateTestID()
	subnetID := ids.GenerateTestID()
	now := time.Unix(1000, 0)

	// Load validator on both Primary Network and a subnet
	primaryMetadata := newTestValidatorMetadata(primaryTxID, time.Hour, now, 1000)
	subnetMetadata := newTestValidatorMetadata(subnetTxID, 0, time.Time{}, 2000)
	require.NoError(state.LoadValidatorMetadata(nodeID, constants.PrimaryNetworkID, primaryMetadata))
	require.NoError(state.LoadValidatorMetadata(nodeID, subnetID, subnetMetadata))

	// Set dirty values for both
	require.NoError(state.SetUptime(nodeID, 2*time.Hour, now))
	require.NoError(state.SetValidatorMutables(nodeID, subnetID, ValidatorMutables{DelegateeReward: 5000}))

	// Write to database
	require.NoError(state.Commit(CodecVersion1))

	// Primary Network entry in primaryDB
	_, err := primaryDB.Get(primaryTxID[:])
	require.NoError(err)
	_, err = subnetDB.Get(primaryTxID[:])
	require.ErrorIs(err, database.ErrNotFound)

	// Subnet entry in subnetDB
	_, err = subnetDB.Get(subnetTxID[:])
	require.NoError(err)
	_, err = primaryDB.Get(subnetTxID[:])
	require.ErrorIs(err, database.ErrNotFound)
}

// Test that Commit with no dirty changes is a no-op
func TestValidatorState_WriteNoDirtyChanges(t *testing.T) {
	require := require.New(t)

	primaryDB := memdb.New()
	subnetDB := memdb.New()
	state := newValidatorState(primaryDB, subnetDB)

	nodeID := ids.GenerateTestNodeID()
	txID := ids.GenerateTestID()
	now := time.Unix(1000, 0)

	// Load validator but don't modify
	metadata := newTestValidatorMetadata(txID, time.Hour, now, 1000)
	require.NoError(state.LoadValidatorMetadata(nodeID, constants.PrimaryNetworkID, metadata))

	// Write with no dirty changes
	require.NoError(state.Commit(CodecVersion1))

	// Database should still be empty (no writes for clean data)
	_, err := primaryDB.Get(txID[:])
	require.ErrorIs(err, database.ErrNotFound)
}

// Test combined uptime and mutables changes in single write
func TestValidatorState_CombinedUptimeAndMutables(t *testing.T) {
	require := require.New(t)

	primaryDB := memdb.New()
	state := newValidatorState(primaryDB, memdb.New())

	nodeID := ids.GenerateTestNodeID()
	txID := ids.GenerateTestID()

	// Load validator on Primary Network
	metadata := newTestValidatorMetadata(txID, time.Hour, time.Unix(1000, 0), 1000)
	require.NoError(state.LoadValidatorMetadata(nodeID, constants.PrimaryNetworkID, metadata))

	// Modify both uptime and mutables
	require.NoError(state.SetUptime(nodeID, 5*time.Hour, time.Unix(5000, 0)))
	require.NoError(state.SetValidatorMutables(nodeID, constants.PrimaryNetworkID, ValidatorMutables{DelegateeReward: 9999}))

	// Both dirty values visible before write
	upDuration, lastUpdated, err := state.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(5*time.Hour, upDuration)
	require.Equal(time.Unix(5000, 0), lastUpdated)

	mutables, err := state.GetValidatorMutables(nodeID, constants.PrimaryNetworkID)
	require.NoError(err)
	require.Equal(uint64(9999), mutables.DelegateeReward)

	// Write persists both
	require.NoError(state.Commit(CodecVersion1))

	// Verify entry was written
	metadataBytes, err := primaryDB.Get(txID[:])
	require.NoError(err)

	vdrMetadata := validatorMetadata{}
	require.NoError(parseValidatorMetadata(metadataBytes, &vdrMetadata))

	require.Equal(uint64(9999), vdrMetadata.PotentialDelegateeReward)
	require.Equal(5*time.Hour, vdrMetadata.UpDuration)
	require.Equal(time.Unix(5000, 0), vdrMetadata.lastUpdated)
}

// Test that deleted validator cannot be updated
func TestValidatorState_DeleteThenUpdate(t *testing.T) {
	require := require.New(t)
	state := newTestValidatorState()

	nodeID := ids.GenerateTestNodeID()
	subnetID := constants.PrimaryNetworkID

	// Load validator
	metadata := newTestValidatorMetadata(ids.GenerateTestID(), time.Hour, time.Unix(1000, 0), 1000)
	require.NoError(state.LoadValidatorMetadata(nodeID, subnetID, metadata))

	// Delete validator
	require.NoError(state.DeleteValidatorMetadata(nodeID, subnetID))

	// Attempting to update deleted validator returns ErrNotFound
	err := state.SetUptime(nodeID, 2*time.Hour, time.Unix(2000, 0))
	require.ErrorIs(err, database.ErrNotFound)

	err = state.SetValidatorMutables(nodeID, subnetID, ValidatorMutables{DelegateeReward: 5000})
	require.ErrorIs(err, database.ErrNotFound)
}
