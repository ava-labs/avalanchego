// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

// Tests:
// * Putting a key-node pair in the database
// * Getting a key-node pair from the cache and from the base db
// * Deleting a key-node pair from the database
// * Evicting elements from the cache
// * Flushing the cache
func Test_IntermediateNodeDB(t *testing.T) {
	require := require.New(t)

	n := newNode(ToKey([]byte{0x00}))
	n.setValue(maybe.Some([]byte{byte(0x02)}))
	nodeSize := cacheEntrySize(n.key, n)

	// use exact multiple of node size so require.Equal(1, db.nodeCache.fifo.Len()) is correct later
	cacheSize := nodeSize * 100
	bufferSize := nodeSize * 20

	evictionBatchSize := bufferSize
	baseDB := memdb.New()
	db := newIntermediateNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
		bufferSize,
		evictionBatchSize,
		4,
	)

	// Put a key-node pair
	node1Key := ToKey([]byte{0x01})
	node1 := newNode(node1Key)
	node1.setValue(maybe.Some([]byte{byte(0x01)}))
	require.NoError(db.Put(node1Key, node1))

	// Get the key-node pair from cache
	node1Read, err := db.Get(node1Key)
	require.NoError(err)
	require.Equal(node1, node1Read)

	// Overwrite the key-node pair
	node1Updated := newNode(node1Key)
	node1Updated.setValue(maybe.Some([]byte{byte(0x02)}))
	require.NoError(db.Put(node1Key, node1Updated))

	// Assert the key-node pair was overwritten
	node1Read, err = db.Get(node1Key)
	require.NoError(err)
	require.Equal(node1Updated, node1Read)

	// Delete the key-node pair
	require.NoError(db.Delete(node1Key))
	_, err = db.Get(node1Key)

	// Assert the key-node pair was deleted
	require.Equal(database.ErrNotFound, err)

	// Put elements in the cache until it is full.
	expectedSize := 0
	added := 0
	for {
		key := ToKey([]byte{byte(added)})
		node := newNode(Key{})
		node.setValue(maybe.Some([]byte{byte(added)}))
		newExpectedSize := expectedSize + cacheEntrySize(key, node)
		if newExpectedSize > bufferSize {
			// Don't trigger eviction.
			break
		}

		require.NoError(db.Put(key, node))
		expectedSize = newExpectedSize
		added++
	}

	// Assert cache has expected number of elements
	require.Equal(added, db.writeBuffer.fifo.Len())

	// Put one more element in the cache, which should trigger an eviction
	// of all but 2 elements. 2 elements remain rather than 1 element because of
	// the added key prefix increasing the size tracked by the batch.
	key := ToKey([]byte{byte(added)})
	node := newNode(Key{})
	node.setValue(maybe.Some([]byte{byte(added)}))
	require.NoError(db.Put(key, node))

	// Assert cache has expected number of elements
	require.Equal(1, db.writeBuffer.fifo.Len())
	gotKey, _, ok := db.writeBuffer.fifo.Oldest()
	require.True(ok)
	require.Equal(ToKey([]byte{byte(added)}), gotKey)

	// Get a node from the base database
	// Use an early key that has been evicted from the cache
	_, inCache := db.writeBuffer.Get(node1Key)
	require.False(inCache)
	nodeRead, err := db.Get(node1Key)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x01}), nodeRead.value)

	// Flush the cache.
	require.NoError(db.Flush())

	// Assert the cache is empty
	require.Zero(db.writeBuffer.fifo.Len())

	// Assert the evicted cache elements were written to disk with prefix.
	it := baseDB.NewIteratorWithPrefix(intermediateNodePrefix)
	defer it.Release()

	count := 0
	for it.Next() {
		count++
	}
	require.NoError(it.Error())
	require.Equal(added+1, count)
}

func FuzzIntermediateNodeDBConstructDBKey(f *testing.F) {
	bufferSize := 200
	cacheSize := 200
	evictionBatchSize := bufferSize
	baseDB := memdb.New()

	f.Fuzz(func(
		t *testing.T,
		key []byte,
		tokenLength uint,
	) {
		require := require.New(t)
		for _, tokenSize := range validTokenSizes {
			db := newIntermediateNodeDB(
				baseDB,
				&sync.Pool{
					New: func() interface{} { return make([]byte, 0) },
				},
				&mockMetrics{},
				cacheSize,
				bufferSize,
				evictionBatchSize,
				tokenSize,
			)

			p := ToKey(key)
			uBitLength := tokenLength * uint(tokenSize)
			if uBitLength >= uint(p.length) {
				t.SkipNow()
			}
			p = p.Take(int(uBitLength))
			constructedKey := db.constructDBKey(p)
			baseLength := len(p.value) + len(intermediateNodePrefix)
			require.Equal(intermediateNodePrefix, constructedKey[:len(intermediateNodePrefix)])
			switch {
			case tokenSize == 8:
				// for keys with tokens of size byte, no padding is added
				require.Equal(p.Bytes(), constructedKey[len(intermediateNodePrefix):])
			case p.hasPartialByte():
				require.Len(constructedKey, baseLength)
				require.Equal(p.Extend(ToToken(1, tokenSize)).Bytes(), constructedKey[len(intermediateNodePrefix):])
			default:
				// when a whole number of bytes, there is an extra padding byte
				require.Len(constructedKey, baseLength+1)
				require.Equal(p.Extend(ToToken(1, tokenSize)).Bytes(), constructedKey[len(intermediateNodePrefix):])
			}
		}
	})
}

func Test_IntermediateNodeDB_ConstructDBKey_DirtyBuffer(t *testing.T) {
	require := require.New(t)
	cacheSize := 200
	bufferSize := 200
	evictionBatchSize := bufferSize
	baseDB := memdb.New()
	db := newIntermediateNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
		bufferSize,
		evictionBatchSize,
		4,
	)

	db.bufferPool.Put([]byte{0xFF, 0xFF, 0xFF})
	constructedKey := db.constructDBKey(ToKey([]byte{}))
	require.Len(constructedKey, 2)
	require.Equal(intermediateNodePrefix, constructedKey[:len(intermediateNodePrefix)])
	require.Equal(byte(16), constructedKey[len(constructedKey)-1])

	db.bufferPool = &sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, defaultBufferLength)
		},
	}
	db.bufferPool.Put([]byte{0xFF, 0xFF, 0xFF})
	p := ToKey([]byte{0xF0}).Take(4)
	constructedKey = db.constructDBKey(p)
	require.Len(constructedKey, 2)
	require.Equal(intermediateNodePrefix, constructedKey[:len(intermediateNodePrefix)])
	require.Equal(p.Extend(ToToken(1, 4)).Bytes(), constructedKey[len(intermediateNodePrefix):])
}

func TestIntermediateNodeDBClear(t *testing.T) {
	require := require.New(t)
	cacheSize := 200
	bufferSize := 200
	evictionBatchSize := bufferSize
	baseDB := memdb.New()
	db := newIntermediateNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
		bufferSize,
		evictionBatchSize,
		4,
	)

	for _, b := range [][]byte{{1}, {2}, {3}} {
		require.NoError(db.Put(ToKey(b), newNode(ToKey(b))))
	}

	require.NoError(db.Clear())

	iter := baseDB.NewIteratorWithPrefix(intermediateNodePrefix)
	defer iter.Release()
	require.False(iter.Next())

	require.Zero(db.writeBuffer.currentSize)
}

// Test that deleting the empty key and flushing works correctly.
// Previously, there was a bug that occurred when deleting the empty key
// if the cache was empty. The size of the cache entry was reported as 0,
// which caused the cache's currentSize to be 0, so on resize() we didn't
// call onEviction. This caused the empty key to not be deleted from the baseDB.
func TestIntermediateNodeDBDeleteEmptyKey(t *testing.T) {
	require := require.New(t)
	cacheSize := 200
	bufferSize := 200
	evictionBatchSize := bufferSize
	baseDB := memdb.New()
	db := newIntermediateNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
		bufferSize,
		evictionBatchSize,
		4,
	)

	emptyKey := ToKey([]byte{})
	require.NoError(db.Put(emptyKey, newNode(emptyKey)))
	require.NoError(db.Flush())

	emptyDBKey := db.constructDBKey(emptyKey)
	has, err := baseDB.Has(emptyDBKey)
	require.NoError(err)
	require.True(has)

	require.NoError(db.Delete(ToKey([]byte{})))
	require.NoError(db.Flush())

	emptyDBKey = db.constructDBKey(emptyKey)
	has, err = baseDB.Has(emptyDBKey)
	require.NoError(err)
	require.False(has)
}
