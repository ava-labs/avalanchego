// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

func TestUptimeTrackerState_SetGetUpdateDelete(t *testing.T) {
	require := require.New(t)

	tracker := NewUptimeTrackerState(memdb.New())

	nodeID := ids.GenerateTestNodeID()

	// Not found
	_, _, err := tracker.GetUptime(nodeID)
	require.ErrorIs(err, database.ErrNotFound)

	// Set uptime
	upDuration := 10 * time.Hour
	lastUpdated := time.Unix(1000, 0)
	tracker.SetUptime(nodeID, upDuration, lastUpdated)

	// Get uptime returns correct values
	gotDuration, gotLastUpdated, err := tracker.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(upDuration, gotDuration)
	require.Equal(lastUpdated, gotLastUpdated)

	// Update uptime
	newDuration := 20 * time.Hour
	newLastUpdated := time.Unix(2000, 0)
	tracker.SetUptime(nodeID, newDuration, newLastUpdated)

	// Get returns updated values
	gotDuration, gotLastUpdated, err = tracker.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(newDuration, gotDuration)
	require.Equal(newLastUpdated, gotLastUpdated)

	// Delete uptime
	tracker.DeleteUptime(nodeID)

	// GetUptime returns ErrNotFound after delete
	_, _, err = tracker.GetUptime(nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestUptimeTrackerState_LoadAndWriteUptime(t *testing.T) {
	require := require.New(t)

	db := memdb.New()

	nodeID := ids.GenerateTestNodeID()
	upDuration := 10 * time.Hour
	lastUpdated := time.Unix(1000, 0)

	// LoadUptime returns ErrNotFound when nothing in DB
	tracker1 := NewUptimeTrackerState(db)

	// Set and write uptime
	tracker1.SetUptime(nodeID, upDuration, lastUpdated)
	require.NoError(tracker1.WriteUptime(UptimeCodecVersion0))

	// New tracker can load persisted data
	tracker2 := NewUptimeTrackerState(db)
	require.NoError(tracker2.LoadUptime(nodeID))

	gotDuration, gotLastUpdated, err := tracker2.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(upDuration, gotDuration)
	require.Equal(lastUpdated, gotLastUpdated)

	// Update data loaded from db.
	upDuration = 20 * time.Hour
	lastUpdated = time.Unix(2000, 0)
	tracker2.SetUptime(nodeID, upDuration, lastUpdated)

	// Check in-memory returned data.
	gotDuration, gotLastUpdated, err = tracker2.GetUptime(nodeID)
	require.NoError(err)
	require.Equal(upDuration, gotDuration)
	require.Equal(lastUpdated, gotLastUpdated)
}

func TestUptimeTrackerState_WriteDeletesFromDB(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	tracker := NewUptimeTrackerState(db)

	nodeID := ids.GenerateTestNodeID()

	// Set and write
	tracker.SetUptime(nodeID, 5*time.Hour, time.Unix(500, 0))
	require.NoError(tracker.WriteUptime(UptimeCodecVersion0))

	// Delete and write
	tracker.DeleteUptime(nodeID)
	require.NoError(tracker.WriteUptime(UptimeCodecVersion0))

	// New tracker can't load deleted data
	tracker2 := NewUptimeTrackerState(db)
	err := tracker2.LoadUptime(nodeID)
	require.ErrorIs(err, database.ErrNotFound)
}
