// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"crypto/rand"
	"testing"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

// SharedMemoryTests is a list of all shared memory tests
var SharedMemoryTests = []func(t *testing.T, chainID0, chainID1 ids.ID, sm0, sm1 SharedMemory, db database.Database){
	TestSharedMemoryPutAndGet,
	TestSharedMemoryIndexed,
	TestSharedMemoryCantDuplicatePut,
	TestSharedMemoryCantDuplicateRemove,
	TestSharedMemoryCommitOnPut,
	TestSharedMemoryCommitOnRemove,
	TestSharedMemoryLargeSize,
}

func TestSharedMemoryPutAndGet(t *testing.T, chainID0, chainID1 ids.ID, sm0, sm1 SharedMemory, _ database.Database) {
	assert := assert.New(t)

	err := sm0.Put(chainID1, []*Element{{
		Key:   []byte{0},
		Value: []byte{1},
	}})
	assert.NoError(err)

	values, err := sm1.Get(chainID0, [][]byte{{0}})
	assert.NoError(err)
	assert.Equal([][]byte{{1}}, values, "wrong values returned")
}

func TestSharedMemoryIndexed(t *testing.T, chainID0, chainID1 ids.ID, sm0, sm1 SharedMemory, _ database.Database) {
	assert := assert.New(t)

	err := sm0.Put(chainID1, []*Element{{
		Key:   []byte{0},
		Value: []byte{1},
		Traits: [][]byte{
			{2},
			{3},
		},
	}})
	assert.NoError(err)

	err = sm0.Put(chainID1, []*Element{{
		Key:   []byte{4},
		Value: []byte{5},
		Traits: [][]byte{
			{2},
			{3},
		},
	}})
	assert.NoError(err)

	values, _, _, err := sm0.Indexed(chainID1, [][]byte{{2}}, nil, nil, 1)
	assert.NoError(err)
	assert.Empty(values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 0)
	assert.NoError(err)
	assert.Empty(values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 1)
	assert.NoError(err)
	assert.Equal([][]byte{{1}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 2)
	assert.NoError(err)
	assert.Equal([][]byte{{1}, {5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 3)
	assert.NoError(err)
	assert.Equal([][]byte{{1}, {5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{3}}, nil, nil, 3)
	assert.NoError(err)
	assert.Equal([][]byte{{1}, {5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}, {3}}, nil, nil, 3)
	assert.NoError(err)
	assert.Equal([][]byte{{1}, {5}}, values, "wrong indexed values returned")
}

func TestSharedMemoryCantDuplicatePut(t *testing.T, _, chainID1 ids.ID, sm0, _ SharedMemory, _ database.Database) {
	assert := assert.New(t)

	err := sm0.Put(chainID1, []*Element{
		{
			Key:   []byte{0},
			Value: []byte{1},
		},
		{
			Key:   []byte{0},
			Value: []byte{2},
		},
	})
	assert.Error(err, "shouldn't be able to write duplicated keys")

	err = sm0.Put(chainID1, []*Element{{
		Key:   []byte{0},
		Value: []byte{1},
	}})
	assert.NoError(err)

	err = sm0.Put(chainID1, []*Element{{
		Key:   []byte{0},
		Value: []byte{1},
	}})
	assert.Error(err, "shouldn't be able to write duplicated keys")
}

func TestSharedMemoryCantDuplicateRemove(t *testing.T, _, chainID1 ids.ID, sm0, _ SharedMemory, _ database.Database) {
	assert := assert.New(t)

	err := sm0.Remove(chainID1, [][]byte{{0}})
	assert.NoError(err)

	err = sm0.Remove(chainID1, [][]byte{{0}})
	assert.Error(err, "shouldn't be able to remove duplicated keys")
}

func TestSharedMemoryCommitOnPut(t *testing.T, _, chainID1 ids.ID, sm0, _ SharedMemory, db database.Database) {
	assert := assert.New(t)

	err := db.Put([]byte{1}, []byte{2})
	assert.NoError(err)

	batch := db.NewBatch()

	err = batch.Put([]byte{0}, []byte{1})
	assert.NoError(err)

	err = batch.Delete([]byte{1})
	assert.NoError(err)

	err = sm0.Put(
		chainID1,
		[]*Element{{
			Key:   []byte{0},
			Value: []byte{1},
		}},
		batch,
	)
	assert.NoError(err)

	val, err := db.Get([]byte{0})
	assert.NoError(err)
	assert.Equal([]byte{1}, val)

	has, err := db.Has([]byte{1})
	assert.NoError(err)
	assert.False(has)
}

func TestSharedMemoryCommitOnRemove(t *testing.T, _, chainID1 ids.ID, sm0, _ SharedMemory, db database.Database) {
	assert := assert.New(t)

	err := db.Put([]byte{1}, []byte{2})
	assert.NoError(err)

	batch := db.NewBatch()

	err = batch.Put([]byte{0}, []byte{1})
	assert.NoError(err)

	err = batch.Delete([]byte{1})
	assert.NoError(err)

	err = sm0.Remove(
		chainID1,
		[][]byte{{0}},
		batch,
	)
	assert.NoError(err)

	val, err := db.Get([]byte{0})
	assert.NoError(err)
	assert.Equal([]byte{1}, val)

	has, err := db.Has([]byte{1})
	assert.NoError(err)
	assert.False(has)
}

// TestSharedMemoryLargeSize tests to make sure that the interface can support
// large batches.
func TestSharedMemoryLargeSize(t *testing.T, _, chainID1 ids.ID, sm0, _ SharedMemory, db database.Database) {
	assert := assert.New(t)

	totalSize := 8 * 1024 * 1024 // 8 MiB
	elementSize := 4 * 1024      // 4 KiB
	pairSize := 2 * elementSize  // 8 KiB

	bytes := make([]byte, totalSize)
	_, err := rand.Read(bytes)
	if err != nil {
		t.Fatal(err)
	}

	batch := db.NewBatch()
	assert.NotNil(batch)

	for len(bytes) > pairSize {
		key := bytes[:elementSize]
		bytes = bytes[elementSize:]

		value := bytes[:elementSize]
		bytes = bytes[elementSize:]

		err := batch.Put(key, value)
		assert.NoError(err)
	}

	err = db.Put([]byte{1}, []byte{2})
	assert.NoError(err)

	err = batch.Put([]byte{0}, []byte{1})
	assert.NoError(err)

	err = batch.Delete([]byte{1})
	assert.NoError(err)

	err = sm0.Remove(
		chainID1,
		[][]byte{{0}},
		batch,
	)
	assert.NoError(err)

	val, err := db.Get([]byte{0})
	assert.NoError(err)
	assert.Equal([]byte{1}, val)

	has, err := db.Has([]byte{1})
	assert.NoError(err)
	assert.False(has)
}
