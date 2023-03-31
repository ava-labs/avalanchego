// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

func TestValidatorUptimes(t *testing.T) {
	require := require.New(t)
	uptimes := newValidatorUptimes()

	// get non-existent uptime
	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()
	_, _, err := uptimes.GetUptime(nodeID, subnetID)
	require.ErrorIs(err, database.ErrNotFound)

	// set non-existent uptime
	err = uptimes.SetUptime(nodeID, subnetID, 1, time.Now())
	require.ErrorIs(err, database.ErrNotFound)

	testUptimeReward := &uptimeAndReward{
		UpDuration:  time.Hour,
		lastUpdated: time.Now(),
	}
	// load uptime
	uptimes.LoadUptime(nodeID, subnetID, testUptimeReward)

	// get uptime
	upDuration, lastUpdated, err := uptimes.GetUptime(nodeID, subnetID)
	require.NoError(err)
	require.Equal(testUptimeReward.UpDuration, upDuration)
	require.Equal(testUptimeReward.lastUpdated, lastUpdated)

	// set uptime
	newUpDuration := testUptimeReward.UpDuration + 1
	newLastUpdated := testUptimeReward.lastUpdated.Add(time.Hour)
	err = uptimes.SetUptime(nodeID, subnetID, newUpDuration, newLastUpdated)
	require.NoError(err)

	// get new uptime
	upDuration, lastUpdated, err = uptimes.GetUptime(nodeID, subnetID)
	require.NoError(err)
	require.Equal(newUpDuration, upDuration)
	require.Equal(newLastUpdated, lastUpdated)

	// load uptime changes uptimes
	newTestUptimeReward := &uptimeAndReward{
		UpDuration:  testUptimeReward.UpDuration + time.Hour,
		lastUpdated: testUptimeReward.lastUpdated.Add(time.Hour),
	}
	uptimes.LoadUptime(nodeID, subnetID, newTestUptimeReward)

	// get new uptime
	upDuration, lastUpdated, err = uptimes.GetUptime(nodeID, subnetID)
	require.NoError(err)
	require.Equal(newTestUptimeReward.UpDuration, upDuration)
	require.Equal(newTestUptimeReward.lastUpdated, lastUpdated)

	// delete uptime
	uptimes.DeleteUptime(nodeID, subnetID)

	// get deleted uptime
	_, _, err = uptimes.GetUptime(nodeID, subnetID)
	require.ErrorIs(err, database.ErrNotFound)
}

func TestWriteUptimes(t *testing.T) {
	require := require.New(t)
	uptimes := newValidatorUptimes()

	primaryDB := memdb.New()
	subnetDB := memdb.New()
	// write empty uptimes
	err := uptimes.WriteUptimes(primaryDB, subnetDB)
	require.NoError(err)

	// load uptime
	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()
	testUptimeReward := &uptimeAndReward{
		UpDuration:      time.Hour,
		lastUpdated:     time.Now(),
		PotentialReward: 100,
		txID:            ids.GenerateTestID(),
	}
	uptimes.LoadUptime(nodeID, subnetID, testUptimeReward)

	// write uptimes, should not reflect to DB yet
	err = uptimes.WriteUptimes(primaryDB, subnetDB)
	require.NoError(err)
	require.False(primaryDB.Has(testUptimeReward.txID[:]))
	require.False(subnetDB.Has(testUptimeReward.txID[:]))

	// get uptime should still return the loaded value
	upDuration, lastUpdated, err := uptimes.GetUptime(nodeID, subnetID)
	require.NoError(err)
	require.Equal(testUptimeReward.UpDuration, upDuration)
	require.Equal(testUptimeReward.lastUpdated, lastUpdated)

	// update uptimes
	newUpDuration := testUptimeReward.UpDuration + 1
	newLastUpdated := testUptimeReward.lastUpdated.Add(time.Hour)
	err = uptimes.SetUptime(nodeID, subnetID, newUpDuration, newLastUpdated)
	require.NoError(err)

	// write uptimes, should reflect to subnet DB
	err = uptimes.WriteUptimes(primaryDB, subnetDB)
	require.NoError(err)
	require.False(primaryDB.Has(testUptimeReward.txID[:]))
	require.True(subnetDB.Has(testUptimeReward.txID[:]))
}
