// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/stretchr/testify/require"
)

// TestFetchCheckpointPersistence tests that checkpoints can be saved and loaded correctly
func TestFetchCheckpointPersistence(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	// Create a checkpoint
	now := time.Now()
	checkpoint := &FetchCheckpoint{
		Height:              500000,
		TipHeight:           1000000,
		StartingHeight:      100000,
		NumBlocksFetched:    400000,
		Timestamp:           now,
		MissingBlockIDCount: 42,
		ETASamples: []timer.Sample{
			{Completed: 100000, Timestamp: now.Add(-4 * time.Hour)},
			{Completed: 200000, Timestamp: now.Add(-3 * time.Hour)},
			{Completed: 300000, Timestamp: now.Add(-2 * time.Hour)},
			{Completed: 400000, Timestamp: now.Add(-1 * time.Hour)},
			{Completed: 500000, Timestamp: now},
		},
	}

	// Save checkpoint
	err := PutFetchCheckpoint(db, checkpoint)
	require.NoError(err)

	// Load checkpoint
	loaded, err := GetFetchCheckpoint(db)
	require.NoError(err)
	require.NotNil(loaded)

	// Verify all fields
	require.Equal(checkpoint.Height, loaded.Height)
	require.Equal(checkpoint.TipHeight, loaded.TipHeight)
	require.Equal(checkpoint.StartingHeight, loaded.StartingHeight)
	require.Equal(checkpoint.NumBlocksFetched, loaded.NumBlocksFetched)
	require.Equal(checkpoint.MissingBlockIDCount, loaded.MissingBlockIDCount)

	// Verify timestamp (within 1ms tolerance for JSON serialization)
	timeDiff := checkpoint.Timestamp.Sub(loaded.Timestamp)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	require.Less(timeDiff, time.Millisecond)

	// Verify ETA samples
	require.Len(loaded.ETASamples, 5)
	for i := 0; i < 5; i++ {
		require.Equal(checkpoint.ETASamples[i].Completed, loaded.ETASamples[i].Completed)

		// Check timestamp (within 1ms tolerance)
		sampleTimeDiff := checkpoint.ETASamples[i].Timestamp.Sub(loaded.ETASamples[i].Timestamp)
		if sampleTimeDiff < 0 {
			sampleTimeDiff = -sampleTimeDiff
		}
		require.Less(sampleTimeDiff, time.Millisecond)
	}
}

// TestFetchCheckpointDelete tests that checkpoints can be deleted
func TestFetchCheckpointDelete(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	// Create and save checkpoint
	checkpoint := &FetchCheckpoint{
		Height:              500000,
		TipHeight:           1000000,
		StartingHeight:      100000,
		NumBlocksFetched:    400000,
		Timestamp:           time.Now(),
		MissingBlockIDCount: 0,
		ETASamples:          []timer.Sample{},
	}

	err := PutFetchCheckpoint(db, checkpoint)
	require.NoError(err)

	// Verify it exists
	loaded, err := GetFetchCheckpoint(db)
	require.NoError(err)
	require.NotNil(loaded)

	// Delete checkpoint
	err = DeleteFetchCheckpoint(db)
	require.NoError(err)

	// Verify it's gone
	loaded, err = GetFetchCheckpoint(db)
	require.NoError(err)
	require.Nil(loaded)
}

// TestFetchCheckpointNotExists tests behavior when checkpoint doesn't exist
func TestFetchCheckpointNotExists(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	// Try to load non-existent checkpoint
	loaded, err := GetFetchCheckpoint(db)
	require.NoError(err)
	require.Nil(loaded)
}

// TestFetchCheckpointEmptySamples tests checkpoint with no ETA samples
func TestFetchCheckpointEmptySamples(t *testing.T) {
	require := require.New(t)
	db := memdb.New()

	checkpoint := &FetchCheckpoint{
		Height:              100000,
		TipHeight:           1000000,
		StartingHeight:      0,
		NumBlocksFetched:    100000,
		Timestamp:           time.Now(),
		MissingBlockIDCount: 0,
		ETASamples:          []timer.Sample{},
	}

	// Save and load
	err := PutFetchCheckpoint(db, checkpoint)
	require.NoError(err)

	loaded, err := GetFetchCheckpoint(db)
	require.NoError(err)
	require.NotNil(loaded)
	require.Len(loaded.ETASamples, 0)
}
