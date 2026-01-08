// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomictest

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
)

// SharedMemoryTests is a list of all shared memory tests
var SharedMemoryTests = []func(t *testing.T, chainID0, chainID1 ids.ID, sm0, sm1 atomic.SharedMemory, db database.Database){
	TestSharedMemoryPutAndGet,
	TestSharedMemoryLargePutGetAndRemove,
	TestSharedMemoryIndexed,
	TestSharedMemoryLargeIndexed,
	TestSharedMemoryCantDuplicatePut,
	TestSharedMemoryCantDuplicateRemove,
	TestSharedMemoryCommitOnPut,
	TestSharedMemoryCommitOnRemove,
	TestSharedMemoryLargeBatchSize,
	TestPutAndRemoveBatch,
}

func TestSharedMemoryPutAndGet(t *testing.T, chainID0, chainID1 ids.ID, sm0, sm1 atomic.SharedMemory, _ database.Database) {
	require := require.New(t)

	require.NoError(sm0.Apply(map[ids.ID]*atomic.Requests{chainID1: {PutRequests: []*atomic.Element{{
		Key:   []byte{0},
		Value: []byte{1},
	}}}}))

	values, err := sm1.Get(chainID0, [][]byte{{0}})
	require.NoError(err)
	require.Equal([][]byte{{1}}, values, "wrong values returned")
}

// TestSharedMemoryLargePutGetAndRemove tests to make sure that the interface
// can support large values.
func TestSharedMemoryLargePutGetAndRemove(t *testing.T, chainID0, chainID1 ids.ID, sm0, sm1 atomic.SharedMemory, _ database.Database) {
	require := require.New(t)
	rand := rand.New(rand.NewSource(0)) //#nosec G404

	totalSize := 16 * units.MiB  // 16 MiB
	elementSize := 4 * units.KiB // 4 KiB
	pairSize := 2 * elementSize  // 8 KiB

	b := make([]byte, totalSize)
	_, err := rand.Read(b) // #nosec G404
	require.NoError(err)

	elems := []*atomic.Element{}
	keys := [][]byte{}
	for len(b) > pairSize {
		key := b[:elementSize]
		b = b[elementSize:]

		value := b[:elementSize]
		b = b[elementSize:]

		elems = append(elems, &atomic.Element{
			Key:   key,
			Value: value,
		})
		keys = append(keys, key)
	}

	require.NoError(sm0.Apply(map[ids.ID]*atomic.Requests{
		chainID1: {
			PutRequests: elems,
		},
	}))

	values, err := sm1.Get(
		chainID0,
		keys,
	)
	require.NoError(err)
	for i, value := range values {
		require.Equal(elems[i].Value, value)
	}

	require.NoError(sm1.Apply(map[ids.ID]*atomic.Requests{
		chainID0: {
			RemoveRequests: keys,
		},
	}))
}

func TestSharedMemoryIndexed(t *testing.T, chainID0, chainID1 ids.ID, sm0, sm1 atomic.SharedMemory, _ database.Database) {
	require := require.New(t)

	require.NoError(sm0.Apply(map[ids.ID]*atomic.Requests{chainID1: {PutRequests: []*atomic.Element{{
		Key:   []byte{0},
		Value: []byte{1},
		Traits: [][]byte{
			{2},
			{3},
		},
	}}}}))

	require.NoError(sm0.Apply(map[ids.ID]*atomic.Requests{chainID1: {PutRequests: []*atomic.Element{{
		Key:   []byte{4},
		Value: []byte{5},
		Traits: [][]byte{
			{2},
			{3},
		},
	}}}}))

	values, _, _, err := sm0.Indexed(chainID1, [][]byte{{2}}, nil, nil, 1)
	require.NoError(err)
	require.Empty(values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 0)
	require.NoError(err)
	require.Empty(values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 1)
	require.NoError(err)
	require.Equal([][]byte{{5}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 2)
	require.NoError(err)
	require.Equal([][]byte{{5}, {1}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}}, nil, nil, 3)
	require.NoError(err)
	require.Equal([][]byte{{5}, {1}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{3}}, nil, nil, 3)
	require.NoError(err)
	require.Equal([][]byte{{5}, {1}}, values, "wrong indexed values returned")

	values, _, _, err = sm1.Indexed(chainID0, [][]byte{{2}, {3}}, nil, nil, 3)
	require.NoError(err)
	require.Equal([][]byte{{5}, {1}}, values, "wrong indexed values returned")
}

func TestSharedMemoryLargeIndexed(t *testing.T, chainID0, chainID1 ids.ID, sm0, sm1 atomic.SharedMemory, _ database.Database) {
	require := require.New(t)

	totalSize := 8 * units.MiB   // 8 MiB
	elementSize := 1 * units.KiB // 1 KiB
	pairSize := 3 * elementSize  // 3 KiB

	b := make([]byte, totalSize)
	_, err := rand.Read(b) // #nosec G404
	require.NoError(err)

	elems := []*atomic.Element{}
	allTraits := [][]byte{}
	for len(b) > pairSize {
		key := b[:elementSize]
		b = b[elementSize:]

		value := b[:elementSize]
		b = b[elementSize:]

		traits := [][]byte{
			b[:elementSize],
		}
		allTraits = append(allTraits, traits...)
		b = b[elementSize:]

		elems = append(elems, &atomic.Element{
			Key:    key,
			Value:  value,
			Traits: traits,
		})
	}

	require.NoError(sm0.Apply(map[ids.ID]*atomic.Requests{chainID1: {PutRequests: elems}}))

	values, _, _, err := sm1.Indexed(chainID0, allTraits, nil, nil, len(elems)+1)
	require.NoError(err)
	require.Len(values, len(elems), "wrong number of values returned")
}

func TestSharedMemoryCantDuplicatePut(t *testing.T, _, chainID1 ids.ID, sm0, _ atomic.SharedMemory, _ database.Database) {
	require := require.New(t)

	err := sm0.Apply(map[ids.ID]*atomic.Requests{chainID1: {PutRequests: []*atomic.Element{
		{
			Key:   []byte{0},
			Value: []byte{1},
		},
		{
			Key:   []byte{0},
			Value: []byte{2},
		},
	}}})
	// TODO: require error to be errDuplicatedOperation
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too

	require.NoError(sm0.Apply(map[ids.ID]*atomic.Requests{chainID1: {PutRequests: []*atomic.Element{{
		Key:   []byte{0},
		Value: []byte{1},
	}}}}))

	err = sm0.Apply(map[ids.ID]*atomic.Requests{chainID1: {PutRequests: []*atomic.Element{{
		Key:   []byte{0},
		Value: []byte{1},
	}}}})
	// TODO: require error to be errDuplicatedOperation
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

func TestSharedMemoryCantDuplicateRemove(t *testing.T, _, chainID1 ids.ID, sm0, _ atomic.SharedMemory, _ database.Database) {
	require := require.New(t)

	require.NoError(sm0.Apply(map[ids.ID]*atomic.Requests{chainID1: {RemoveRequests: [][]byte{{0}}}}))

	err := sm0.Apply(map[ids.ID]*atomic.Requests{chainID1: {RemoveRequests: [][]byte{{0}}}})
	// TODO: require error to be errDuplicatedOperation
	require.Error(err) //nolint:forbidigo // currently returns grpc errors too
}

func TestSharedMemoryCommitOnPut(t *testing.T, _, chainID1 ids.ID, sm0, _ atomic.SharedMemory, db database.Database) {
	require := require.New(t)

	require.NoError(db.Put([]byte{1}, []byte{2}))

	batch := db.NewBatch()

	require.NoError(batch.Put([]byte{0}, []byte{1}))

	require.NoError(batch.Delete([]byte{1}))

	require.NoError(sm0.Apply(
		map[ids.ID]*atomic.Requests{chainID1: {PutRequests: []*atomic.Element{{
			Key:   []byte{0},
			Value: []byte{1},
		}}}},
		batch,
	))

	val, err := db.Get([]byte{0})
	require.NoError(err)
	require.Equal([]byte{1}, val)

	has, err := db.Has([]byte{1})
	require.NoError(err)
	require.False(has)
}

func TestSharedMemoryCommitOnRemove(t *testing.T, _, chainID1 ids.ID, sm0, _ atomic.SharedMemory, db database.Database) {
	require := require.New(t)

	require.NoError(db.Put([]byte{1}, []byte{2}))

	batch := db.NewBatch()

	require.NoError(batch.Put([]byte{0}, []byte{1}))

	require.NoError(batch.Delete([]byte{1}))

	require.NoError(sm0.Apply(
		map[ids.ID]*atomic.Requests{chainID1: {RemoveRequests: [][]byte{{0}}}},
		batch,
	))

	val, err := db.Get([]byte{0})
	require.NoError(err)
	require.Equal([]byte{1}, val)

	has, err := db.Has([]byte{1})
	require.NoError(err)
	require.False(has)
}

// TestPutAndRemoveBatch tests to make sure multiple put and remove requests work properly
func TestPutAndRemoveBatch(t *testing.T, chainID0, _ ids.ID, _, sm1 atomic.SharedMemory, db database.Database) {
	require := require.New(t)

	batch := db.NewBatch()

	require.NoError(batch.Put([]byte{0}, []byte{1}))

	batchChainsAndInputs := make(map[ids.ID]*atomic.Requests)

	byteArr := [][]byte{{0}, {1}, {5}}

	batchChainsAndInputs[chainID0] = &atomic.Requests{
		PutRequests: []*atomic.Element{{
			Key:   []byte{2},
			Value: []byte{9},
		}},
		RemoveRequests: byteArr,
	}

	require.NoError(sm1.Apply(batchChainsAndInputs, batch))

	val, err := db.Get([]byte{0})
	require.NoError(err)
	require.Equal([]byte{1}, val)
}

// TestSharedMemoryLargeBatchSize tests to make sure that the interface can
// support large batches.
func TestSharedMemoryLargeBatchSize(t *testing.T, _, chainID1 ids.ID, sm0, _ atomic.SharedMemory, db database.Database) {
	require := require.New(t)
	rand := rand.New(rand.NewSource(0)) //#nosec G404

	totalSize := 8 * units.MiB   // 8 MiB
	elementSize := 4 * units.KiB // 4 KiB
	pairSize := 2 * elementSize  // 8 KiB

	bytes := make([]byte, totalSize)
	_, err := rand.Read(bytes) // #nosec G404
	require.NoError(err)

	batch := db.NewBatch()
	require.NotNil(batch)

	initialBytes := bytes
	for len(bytes) > pairSize {
		key := bytes[:elementSize]
		bytes = bytes[elementSize:]

		value := bytes[:elementSize]
		bytes = bytes[elementSize:]

		require.NoError(batch.Put(key, value))
	}

	require.NoError(db.Put([]byte{1}, []byte{2}))

	require.NoError(batch.Put([]byte{0}, []byte{1}))

	require.NoError(batch.Delete([]byte{1}))

	require.NoError(sm0.Apply(
		map[ids.ID]*atomic.Requests{chainID1: {RemoveRequests: [][]byte{{0}}}},
		batch,
	))

	val, err := db.Get([]byte{0})
	require.NoError(err)
	require.Equal([]byte{1}, val)

	has, err := db.Has([]byte{1})
	require.NoError(err)
	require.False(has)

	batch.Reset()

	bytes = initialBytes
	for len(bytes) > pairSize {
		key := bytes[:elementSize]
		bytes = bytes[pairSize:]

		require.NoError(batch.Delete(key))
	}

	require.NoError(sm0.Apply(
		map[ids.ID]*atomic.Requests{chainID1: {RemoveRequests: [][]byte{{1}}}},
		batch,
	))

	batch.Reset()

	bytes = initialBytes
	for len(bytes) > pairSize {
		key := bytes[:elementSize]
		bytes = bytes[pairSize:]

		require.NoError(batch.Delete(key))
	}

	batchChainsAndInputs := make(map[ids.ID]*atomic.Requests)

	byteArr := [][]byte{{30}, {40}, {50}}

	batchChainsAndInputs[chainID1] = &atomic.Requests{
		PutRequests: []*atomic.Element{{
			Key:   []byte{2},
			Value: []byte{9},
		}},
		RemoveRequests: byteArr,
	}

	require.NoError(sm0.Apply(
		batchChainsAndInputs,
		batch,
	))
}
