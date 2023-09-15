// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	// use exact multiple of node size so require.Equal(1, db.nodeCache.fifo.Len()) is correct later
	cacheSize := 200
	evictionBatchSize := cacheSize
	baseDB := memdb.New()
	db := newIntermediateNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
		evictionBatchSize,
	)

	// Put a key-node pair
	node1Key := newPath([]byte{0x01})
	node1 := newNode(nil, node1Key)
	node1.setValue(maybe.Some([]byte{byte(0x01)}))
	require.NoError(db.Put(node1Key, node1))

	// Get the key-node pair from cache
	node1Read, err := db.Get(node1Key)
	require.NoError(err)
	require.Equal(node1, node1Read)

	// Overwrite the key-node pair
	node1Updated := newNode(nil, node1Key)
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
		key := newPath([]byte{byte(added)})
		node := newNode(nil, key)
		node.setValue(maybe.Some([]byte{byte(added)}))
		newExpectedSize := expectedSize + cacheEntrySize(key, node)
		if newExpectedSize > cacheSize {
			// Don't trigger eviction.
			break
		}

		require.NoError(db.Put(key, node))
		expectedSize = newExpectedSize
		added++
	}

	// Assert cache has expected number of elements
	require.Equal(added, db.nodeCache.fifo.Len())

	// Put one more element in the cache, which should trigger an eviction
	// of all but 2 elements. 2 elements remain rather than 1 element because of
	// the added key prefix increasing the size tracked by the batch.
	key := newPath([]byte{byte(added)})
	node := newNode(nil, key)
	node.setValue(maybe.Some([]byte{byte(added)}))
	require.NoError(db.Put(key, node))

	// Assert cache has expected number of elements
	require.Equal(1, db.nodeCache.fifo.Len())
	gotKey, _, ok := db.nodeCache.fifo.Oldest()
	require.True(ok)
	require.Equal(newPath([]byte{byte(added)}), gotKey)

	// Get a node from the base database
	// Use an early key that has been evicted from the cache
	_, inCache := db.nodeCache.Get(node1Key)
	require.False(inCache)
	nodeRead, err := db.Get(node1Key)
	require.NoError(err)
	require.Equal(maybe.Some([]byte{0x01}), nodeRead.value)

	// Flush the cache.
	require.NoError(db.Flush())

	// Assert the cache is empty
	require.Zero(db.nodeCache.fifo.Len())

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
	cacheSize := 200
	evictionBatchSize := cacheSize
	baseDB := memdb.New()
	db := newIntermediateNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
		evictionBatchSize,
	)

	f.Fuzz(func(
		t *testing.T,
		key []byte,
	) {
		require := require.New(t)

		p := newPath(key)
		constructedKey := db.constructDBKey(p)
		baseLength := len(p)/2 + len(intermediateNodePrefix)
		require.Equal(intermediateNodePrefix, constructedKey[:len(intermediateNodePrefix)])
		if len(p)%2 == 0 {
			// when even, there is an extra padding byte
			require.Len(constructedKey, baseLength+1)
			require.Equal(append(p.Serialize().Value, 0b1000_0000), constructedKey[len(intermediateNodePrefix):])
		} else {
			require.Len(constructedKey, baseLength)
			require.Equal(p.Append(0b0000_1000).Serialize().Value, constructedKey[len(intermediateNodePrefix):])
		}
	})
}

func Test_IntermediateNodeDB_ConstructDBKey_DirtyBuffer(t *testing.T) {
	require := require.New(t)
	cacheSize := 200
	evictionBatchSize := cacheSize
	baseDB := memdb.New()
	db := newIntermediateNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
		evictionBatchSize,
	)

	db.bufferPool.Put([]byte{0xFF, 0xFF, 0xFF})
	constructedKey := db.constructDBKey(newPath([]byte{}))
	require.Len(constructedKey, 2)
	require.Equal(intermediateNodePrefix, constructedKey[:len(intermediateNodePrefix)])
	require.Equal(byte(0b1000_0000), constructedKey[len(constructedKey)-1])

	db.bufferPool = &sync.Pool{
		New: func() interface{} {
			return make([]byte, 0, defaultBufferLength)
		},
	}
	db.bufferPool.Put([]byte{0xFF, 0xFF, 0xFF})
	p := path(" ")
	constructedKey = db.constructDBKey(p)
	require.Len(constructedKey, 2)
	require.Equal(intermediateNodePrefix, constructedKey[:len(intermediateNodePrefix)])
	require.Equal(p.Append(0b0000_1000).Serialize().Value, constructedKey[len(intermediateNodePrefix):])
}
