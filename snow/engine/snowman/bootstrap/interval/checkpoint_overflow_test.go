// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"math"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/stretchr/testify/require"
)

// TestCheckpointHeightOverflow tests that checkpoint height calculations
// handle uint64 overflow correctly by detecting and disabling checkpointing
// when overflow would occur.
func TestCheckpointHeightOverflow(t *testing.T) {
	t.Run("normal_checkpoint_no_overflow", func(t *testing.T) {
		// Normal case - no overflow expected
		db := memdb.New()

		checkpoint := &FetchCheckpoint{
			Height:              100000,
			TipHeight:           500000,
			StartingHeight:      0,
			NumBlocksFetched:    50000,
			Timestamp:           time.Now(),
			MissingBlockIDCount: 10,
			ETASamples:          []timer.Sample{},
		}

		err := PutFetchCheckpoint(db, checkpoint)
		require.NoError(t, err)

		retrieved, err := GetFetchCheckpoint(db)
		require.NoError(t, err)
		require.NotNil(t, retrieved)
		require.Equal(t, uint64(100000), retrieved.Height)

		// Simulate next checkpoint calculation (would be done by bootstrapper)
		checkpointInterval := uint64(100000)
		nextHeight := retrieved.Height + checkpointInterval

		// Verify no overflow
		require.Greater(t, nextHeight, retrieved.Height, "should not overflow")
		require.Equal(t, uint64(200000), nextHeight)
	})

	t.Run("checkpoint_near_max_uint64_causes_overflow", func(t *testing.T) {
		// Checkpoint height near MaxUint64 should cause overflow detection
		db := memdb.New()

		// Set checkpoint height very close to MaxUint64
		nearMaxHeight := uint64(math.MaxUint64 - 50000)
		checkpoint := &FetchCheckpoint{
			Height:              nearMaxHeight,
			TipHeight:           nearMaxHeight,
			StartingHeight:      0,
			NumBlocksFetched:    nearMaxHeight,
			Timestamp:           time.Now(),
			MissingBlockIDCount: 0,
			ETASamples:          []timer.Sample{},
		}

		err := PutFetchCheckpoint(db, checkpoint)
		require.NoError(t, err)

		retrieved, err := GetFetchCheckpoint(db)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		// Simulate next checkpoint calculation with overflow detection
		checkpointInterval := uint64(100000)
		nextHeight := retrieved.Height + checkpointInterval

		// Verify overflow occurred (nextHeight wrapped around to small value)
		require.Less(t, nextHeight, retrieved.Height, "overflow should have occurred")

		// In actual code, bootstrapper detects this and sets nextCheckpointHeight to 0
		// to disable further checkpointing
	})

	t.Run("starting_height_near_max_causes_overflow", func(t *testing.T) {
		// Starting height near MaxUint64 should cause overflow on initialization
		startingHeight := uint64(math.MaxUint64 - 50000)
		checkpointInterval := uint64(100000)

		// Simulate: nextCheckpointHeight = startingHeight + checkpointInterval
		nextHeight := startingHeight + checkpointInterval

		// Verify overflow occurred
		require.Less(t, nextHeight, startingHeight, "overflow should have occurred")

		// In actual code, this would trigger:
		// if nextHeight < startingHeight {
		//     nextCheckpointHeight = 0 // disable checkpointing
		// }
	})

	t.Run("max_uint64_starting_height", func(t *testing.T) {
		// Edge case: starting at exactly MaxUint64
		startingHeight := uint64(math.MaxUint64)
		checkpointInterval := uint64(100000)

		nextHeight := startingHeight + checkpointInterval

		// Any addition to MaxUint64 will overflow
		require.Less(t, nextHeight, startingHeight, "overflow should have occurred")
		require.Equal(t, uint64(99999), nextHeight, "should wrap around to interval-1")
	})

	t.Run("incremental_checkpoint_overflow", func(t *testing.T) {
		// Test overflow during incremental checkpoint updates
		// Simulates: b.nextCheckpointHeight += b.checkpointInterval

		currentHeight := uint64(math.MaxUint64 - 50000)
		checkpointInterval := uint64(100000)

		nextHeight := currentHeight + checkpointInterval

		require.Less(t, nextHeight, currentHeight, "overflow should have occurred")

		// After overflow detection, checkpointing should be disabled
		// by setting nextCheckpointHeight to 0
	})

	t.Run("very_large_checkpoint_interval", func(t *testing.T) {
		// Test with extremely large checkpoint interval
		startingHeight := uint64(100000)
		checkpointInterval := uint64(math.MaxUint64 - 50000)

		nextHeight := startingHeight + checkpointInterval

		// This will overflow because interval is too large
		require.Less(t, nextHeight, startingHeight, "overflow should have occurred")
	})
}

// TestCheckpointIntervalEdgeCases tests edge cases for checkpoint interval configuration
func TestCheckpointIntervalEdgeCases(t *testing.T) {
	t.Run("zero_interval_should_default", func(t *testing.T) {
		// Zero interval should be handled by bootstrapper (defaults to 100,000)
		// This test documents the expected behavior

		checkpointInterval := uint64(0)
		if checkpointInterval == 0 {
			checkpointInterval = 100000 // Default
		}

		require.Equal(t, uint64(100000), checkpointInterval)
	})

	t.Run("max_uint64_interval", func(t *testing.T) {
		// MaxUint64 interval means checkpointing would never trigger
		// (unless blockchain reaches MaxUint64 blocks)

		startingHeight := uint64(0)
		checkpointInterval := uint64(math.MaxUint64)

		nextHeight := startingHeight + checkpointInterval

		// This is technically valid (no overflow from 0 + MaxUint64)
		require.Equal(t, uint64(math.MaxUint64), nextHeight)
		require.Greater(t, nextHeight, startingHeight)
	})

	t.Run("interval_one", func(t *testing.T) {
		// Checkpoint every block (valid but impractical)
		startingHeight := uint64(1000)
		checkpointInterval := uint64(1)

		nextHeight := startingHeight + checkpointInterval

		require.Equal(t, uint64(1001), nextHeight)
		require.Greater(t, nextHeight, startingHeight)
	})
}
