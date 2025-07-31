// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

func TestIndex(t *testing.T) {
	// Setup
	pageSize := uint64(64)
	require := require.New(t)
	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)

	idx, err := newIndex(db, logging.NoLog{}, mockable.Clock{})
	require.NoError(err)

	// Populate "containers" with random IDs/bytes
	containers := map[ids.ID][]byte{}
	for i := uint64(0); i < 2*pageSize; i++ {
		containers[ids.GenerateTestID()] = utils.RandomBytes(32)
	}

	// Accept each container and after each, make assertions
	i := uint64(0)
	for containerID, containerBytes := range containers {
		require.NoError(idx.Accept(ctx, containerID, containerBytes))

		lastAcceptedIndex, ok := idx.lastAcceptedIndex()
		require.True(ok)
		require.Equal(i, lastAcceptedIndex)
		require.Equal(i+1, idx.nextAcceptedIndex)

		gotContainer, err := idx.GetContainerByID(containerID)
		require.NoError(err)
		require.Equal(containerBytes, gotContainer.Bytes)

		gotIndex, err := idx.GetIndex(containerID)
		require.NoError(err)
		require.Equal(i, gotIndex)

		gotContainer, err = idx.GetContainerByIndex(i)
		require.NoError(err)
		require.Equal(containerBytes, gotContainer.Bytes)

		gotContainer, err = idx.GetLastAccepted()
		require.NoError(err)
		require.Equal(containerBytes, gotContainer.Bytes)

		containers, err := idx.GetContainerRange(i, 1)
		require.NoError(err)
		require.Len(containers, 1)
		require.Equal(containerBytes, containers[0].Bytes)

		containers, err = idx.GetContainerRange(i, 2)
		require.NoError(err)
		require.Len(containers, 1)
		require.Equal(containerBytes, containers[0].Bytes)

		i++
	}

	// Create a new index with the same database and ensure contents still there
	require.NoError(db.Commit())
	require.NoError(idx.Close())
	db = versiondb.New(baseDB)
	idx, err = newIndex(db, logging.NoLog{}, mockable.Clock{})
	require.NoError(err)

	// Get all of the containers
	containersList, err := idx.GetContainerRange(0, pageSize)
	require.NoError(err)
	require.Len(containersList, int(pageSize))
	containersList2, err := idx.GetContainerRange(pageSize, pageSize)
	require.NoError(err)
	require.Len(containersList2, int(pageSize))
	containersList = append(containersList, containersList2...)

	// Ensure that the data is correct
	lastTimestamp := int64(0)
	sawContainers := set.Set[ids.ID]{}
	for _, container := range containersList {
		require.False(sawContainers.Contains(container.ID)) // Should only see this container once
		require.Contains(containers, container.ID)
		require.Equal(containers[container.ID], container.Bytes)
		// Timestamps should be non-decreasing
		require.GreaterOrEqual(container.Timestamp, lastTimestamp)
		lastTimestamp = container.Timestamp
		sawContainers.Add(container.ID)
	}
}

func TestIndexGetContainerByRangeMaxPageSize(t *testing.T) {
	// Setup
	require := require.New(t)
	db := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	idx, err := newIndex(db, logging.NoLog{}, mockable.Clock{})
	require.NoError(err)

	// Insert [MaxFetchedByRange] + 1 containers
	for i := uint64(0); i < MaxFetchedByRange+1; i++ {
		require.NoError(idx.Accept(ctx, ids.GenerateTestID(), utils.RandomBytes(32)))
	}

	// Page size too large
	_, err = idx.GetContainerRange(0, MaxFetchedByRange+1)
	require.ErrorIs(err, errNumToFetchInvalid)

	// Make sure data is right
	containers, err := idx.GetContainerRange(0, MaxFetchedByRange)
	require.NoError(err)
	require.Len(containers, MaxFetchedByRange)

	containers2, err := idx.GetContainerRange(1, MaxFetchedByRange)
	require.NoError(err)
	require.Len(containers2, MaxFetchedByRange)

	require.Equal(containers[1], containers2[0])
	require.Equal(containers[MaxFetchedByRange-1], containers2[MaxFetchedByRange-2])

	// Should have last 2 elements
	containers, err = idx.GetContainerRange(MaxFetchedByRange-1, MaxFetchedByRange)
	require.NoError(err)
	require.Len(containers, 2)
	require.Equal(containers[1], containers2[MaxFetchedByRange-1])
	require.Equal(containers[0], containers2[MaxFetchedByRange-2])
}

func TestDontIndexSameContainerTwice(t *testing.T) {
	// Setup
	require := require.New(t)
	db := memdb.New()
	snowCtx := snowtest.Context(t, snowtest.CChainID)
	ctx := snowtest.ConsensusContext(snowCtx)
	idx, err := newIndex(db, logging.NoLog{}, mockable.Clock{})
	require.NoError(err)

	// Accept the same container twice
	containerID := ids.GenerateTestID()
	require.NoError(idx.Accept(ctx, containerID, []byte{1, 2, 3}))
	require.NoError(idx.Accept(ctx, containerID, []byte{4, 5, 6}))
	_, err = idx.GetContainerByIndex(1)
	require.ErrorIs(err, errNoContainerAtIndex)
	gotContainer, err := idx.GetContainerByID(containerID)
	require.NoError(err)
	require.Equal([]byte{1, 2, 3}, gotContainer.Bytes)
}
