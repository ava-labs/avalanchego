package indexer

import (
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/stretchr/testify/assert"
)

func testIsAccepted(_ ids.ID) bool { return true }

func TestIndex(t *testing.T) {
	// Setup
	pageSize := uint64(64)
	assert := assert.New(t)
	codec := codec.NewDefaultManager()
	err := codec.RegisterCodec(codecVersion, linearcodec.NewDefault())
	assert.NoError(err)
	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	ctx := snow.DefaultContextTest()

	indexIntf, err := newIndex(db, logging.NoLog{}, codec, timer.Clock{}, testIsAccepted)
	assert.NoError(err)
	idx := indexIntf.(*index)

	// Populate "containers" with random IDs/bytes
	containers := map[ids.ID][]byte{}
	for i := uint64(0); i < 2*pageSize; i++ {
		containers[ids.GenerateTestID()] = utils.RandomBytes(32)
	}

	// Accept each container and after each, make assertions
	i := uint64(0)
	for containerID, containerBytes := range containers {
		err = idx.Accept(ctx, containerID, containerBytes)
		assert.NoError(err)

		lastAcceptedIndex, ok := idx.lastAcceptedIndex()
		assert.True(ok)
		assert.EqualValues(i, lastAcceptedIndex)
		assert.EqualValues(i+1, idx.nextAcceptedIndex)

		gotContainer, err := idx.GetContainerByID(containerID)
		assert.NoError(err)
		assert.Equal(containerBytes, gotContainer.Bytes)

		gotIndex, err := idx.GetIndex(containerID)
		assert.NoError(err)
		assert.EqualValues(i, gotIndex)

		gotContainer, err = idx.GetContainerByIndex(i)
		assert.NoError(err)
		assert.Equal(containerBytes, gotContainer.Bytes)

		gotContainer, err = idx.GetLastAccepted()
		assert.NoError(err)
		assert.Equal(containerBytes, gotContainer.Bytes)

		containers, err := idx.GetContainerRange(i, 1)
		assert.NoError(err)
		assert.Len(containers, 1)
		assert.Equal(containerBytes, containers[0].Bytes)

		containers, err = idx.GetContainerRange(i, 2)
		assert.NoError(err)
		assert.Len(containers, 1)
		assert.Equal(containerBytes, containers[0].Bytes)

		i++
	}

	// Create a new index with the same database and ensure contents still there
	assert.NoError(db.Commit())
	assert.NoError(idx.Close())
	db = versiondb.New(baseDB)
	indexIntf, err = newIndex(db, logging.NoLog{}, codec, timer.Clock{}, testIsAccepted)
	assert.NoError(err)
	idx = indexIntf.(*index)

	// Get all of the containers
	containersList, err := idx.GetContainerRange(0, pageSize)
	assert.NoError(err)
	assert.Len(containersList, int(pageSize))
	containersList2, err := idx.GetContainerRange(pageSize, pageSize)
	assert.NoError(err)
	assert.Len(containersList2, int(pageSize))
	containersList = append(containersList, containersList2...)

	// Ensure that the data is correct
	lastTimestamp := int64(0)
	sawContainers := ids.Set{}
	for _, container := range containersList {
		assert.False(sawContainers.Contains(container.ID)) // Should only see this container once
		assert.Contains(containers, container.ID)
		assert.EqualValues(containers[container.ID], container.Bytes)
		// Timestamps should be non-decreasing
		assert.True(container.Timestamp >= lastTimestamp)
		lastTimestamp = container.Timestamp
		sawContainers.Add(container.ID)
	}
}

func TestIndexGetContainerByRangeMaxPageSize(t *testing.T) {
	// Setup
	assert := assert.New(t)
	codec := codec.NewDefaultManager()
	err := codec.RegisterCodec(codecVersion, linearcodec.NewDefault())
	assert.NoError(err)
	db := memdb.New()
	ctx := snow.DefaultContextTest()
	indexIntf, err := newIndex(db, logging.NoLog{}, codec, timer.Clock{}, testIsAccepted)
	assert.NoError(err)
	idx := indexIntf.(*index)

	// Insert [maxFetchedByRange] + 1 containers
	for i := uint64(0); i < maxFetchedByRange+1; i++ {
		err = idx.Accept(ctx, ids.GenerateTestID(), utils.RandomBytes(32))
		assert.NoError(err)
	}

	// Page size too large
	_, err = idx.GetContainerRange(0, maxFetchedByRange+1)
	assert.Error(err)

	// Make sure data is right
	containers, err := idx.GetContainerRange(0, maxFetchedByRange)
	assert.NoError(err)
	assert.Len(containers, maxFetchedByRange)

	containers2, err := idx.GetContainerRange(1, maxFetchedByRange)
	assert.NoError(err)
	assert.Len(containers2, maxFetchedByRange)

	assert.Equal(containers[1], containers2[0])
	assert.Equal(containers[maxFetchedByRange-1], containers2[maxFetchedByRange-2])

	// Should have last 2 elements
	containers, err = idx.GetContainerRange(maxFetchedByRange-1, maxFetchedByRange)
	assert.NoError(err)
	assert.Len(containers, 2)
	assert.EqualValues(containers[1], containers2[maxFetchedByRange-1])
	assert.EqualValues(containers[0], containers2[maxFetchedByRange-2])
}

func TestIndexRollBackAccepted(t *testing.T) {
	// Setup
	assert := assert.New(t)
	codec := codec.NewDefaultManager()
	err := codec.RegisterCodec(codecVersion, linearcodec.NewDefault())
	assert.NoError(err)
	baseDB := memdb.New()
	db := versiondb.New(baseDB)
	ctx := snow.DefaultContextTest()
	indexIntf, err := newIndex(db, logging.NoLog{}, codec, timer.Clock{}, testIsAccepted)
	assert.NoError(err)
	idx := indexIntf.(*index)

	// Accept 3 containers
	containers := []ids.ID{}
	for i := uint64(0); i < 3; i++ {
		id := ids.GenerateTestID()
		containers = append(containers, id)
		assert.NoError(idx.Accept(ctx, id, utils.RandomBytes(32)))
	}

	// Close the index
	assert.NoError(db.Commit())
	assert.NoError(idx.Close())

	// Re-open but with a function that says that only the first container is accepted
	db = versiondb.New(baseDB)
	isAccepted := func(containerID ids.ID) bool {
		assert.Contains(containers, containerID)
		return containerID == containers[0]
	}
	indexIntf, err = newIndex(db, logging.NoLog{}, codec, timer.Clock{}, isAccepted)
	assert.NoError(err)
	idx = indexIntf.(*index)

	// Should say that the only accepted container is containers[0]
	index, ok := idx.lastAcceptedIndex()
	assert.True(ok)
	assert.EqualValues(0, index)
	container, err := idx.GetLastAccepted()
	assert.NoError(err)
	assert.Equal(containers[0], container.ID)
	assert.EqualValues(1, idx.nextAcceptedIndex)
	_, err = idx.GetContainerByIndex(1)
	assert.Error(err)
	_, err = idx.GetContainerByIndex(2)
	assert.Error(err)
	_, err = idx.GetContainerByID(containers[1])
	assert.Error(err)
	_, err = idx.GetContainerByID(containers[2])
	assert.Error(err)
}
