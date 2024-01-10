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

// Test putting, modifying, deleting, and getting key-node pairs.
func TestValueNodeDB(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()

	cacheSize := 10_000
	db := newValueNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
	)

	// Getting a key that doesn't exist should return an error.
	key := ToKey([]byte{0x01})
	_, err := db.Get(key)
	require.ErrorIs(err, database.ErrNotFound)

	// Put a key-node pair.
	node1 := &node{
		dbNode: dbNode{
			value: maybe.Some([]byte{0x01}),
		},
		key: key,
	}
	batch := db.NewBatch()
	batch.Put(key, node1)
	require.NoError(batch.Write())

	// Get the key-node pair.
	node1Read, err := db.Get(key)
	require.NoError(err)
	require.Equal(node1, node1Read)

	// Delete the key-node pair.
	batch = db.NewBatch()
	batch.Delete(key)
	require.NoError(batch.Write())

	// Key should be gone now.
	_, err = db.Get(key)
	require.ErrorIs(err, database.ErrNotFound)

	// Put a key-node pair and delete it in the same batch.
	batch = db.NewBatch()
	batch.Put(key, node1)
	batch.Delete(key)
	require.NoError(batch.Write())

	// Key should still be gone.
	_, err = db.Get(key)
	require.ErrorIs(err, database.ErrNotFound)

	// Put a key-node pair and overwrite it in the same batch.
	node2 := &node{
		dbNode: dbNode{
			value: maybe.Some([]byte{0x02}),
		},
		key: key,
	}
	batch = db.NewBatch()
	batch.Put(key, node1)
	batch.Put(key, node2)
	require.NoError(batch.Write())

	// Get the key-node pair.
	node2Read, err := db.Get(key)
	require.NoError(err)
	require.Equal(node2, node2Read)

	// Overwrite the key-node pair in a subsequent batch.
	batch = db.NewBatch()
	batch.Put(key, node1)
	require.NoError(batch.Write())

	// Get the key-node pair.
	node1Read, err = db.Get(key)
	require.NoError(err)
	require.Equal(node1, node1Read)

	// Get the key-node pair from the database, not the cache.
	db.nodeCache.Flush()
	node1Read, err = db.Get(key)
	require.NoError(err)
	// Only check value since we're not setting other node fields.
	require.Equal(node1.value, node1Read.value)

	// Make sure the key is prefixed in the base database.
	it := baseDB.NewIteratorWithPrefix(valueNodePrefix)
	defer it.Release()
	require.True(it.Next())
	require.False(it.Next())
}

func TestValueNodeDBIterator(t *testing.T) {
	require := require.New(t)

	baseDB := memdb.New()
	cacheSize := 10
	db := newValueNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
	)

	// Put key-node pairs.
	for i := 0; i < cacheSize; i++ {
		key := ToKey([]byte{byte(i)})
		node := &node{
			dbNode: dbNode{
				value: maybe.Some([]byte{byte(i)}),
			},
			key: key,
		}
		batch := db.NewBatch()
		batch.Put(key, node)
		require.NoError(batch.Write())
	}

	// Iterate over the key-node pairs.
	it := db.newIteratorWithStartAndPrefix(nil, nil)

	i := 0
	for it.Next() {
		require.Equal([]byte{byte(i)}, it.Key())
		require.Equal([]byte{byte(i)}, it.Value())
		i++
	}
	require.NoError(it.Error())
	require.Equal(cacheSize, i)
	it.Release()

	// Iterate over the key-node pairs with a start.
	it = db.newIteratorWithStartAndPrefix([]byte{2}, nil)
	i = 0
	for it.Next() {
		require.Equal([]byte{2 + byte(i)}, it.Key())
		require.Equal([]byte{2 + byte(i)}, it.Value())
		i++
	}
	require.NoError(it.Error())
	require.Equal(cacheSize-2, i)
	it.Release()

	// Put key-node pairs with a common prefix.
	key := ToKey([]byte{0xFF, 0x00})
	n := &node{
		dbNode: dbNode{
			value: maybe.Some([]byte{0xFF, 0x00}),
		},
		key: key,
	}
	batch := db.NewBatch()
	batch.Put(key, n)
	require.NoError(batch.Write())

	key = ToKey([]byte{0xFF, 0x01})
	n = &node{
		dbNode: dbNode{
			value: maybe.Some([]byte{0xFF, 0x01}),
		},
		key: key,
	}
	batch = db.NewBatch()
	batch.Put(key, n)
	require.NoError(batch.Write())

	// Iterate over the key-node pairs with a prefix.
	it = db.newIteratorWithStartAndPrefix(nil, []byte{0xFF})
	i = 0
	for it.Next() {
		require.Equal([]byte{0xFF, byte(i)}, it.Key())
		require.Equal([]byte{0xFF, byte(i)}, it.Value())
		i++
	}
	require.NoError(it.Error())
	require.Equal(2, i)

	// Iterate over the key-node pairs with a start and prefix.
	it = db.newIteratorWithStartAndPrefix([]byte{0xFF, 0x01}, []byte{0xFF})
	i = 0
	for it.Next() {
		require.Equal([]byte{0xFF, 0x01}, it.Key())
		require.Equal([]byte{0xFF, 0x01}, it.Value())
		i++
	}
	require.NoError(it.Error())
	require.Equal(1, i)

	// Iterate over closed database.
	it = db.newIteratorWithStartAndPrefix(nil, nil)
	require.True(it.Next())
	require.NoError(it.Error())
	db.Close()
	require.False(it.Next())
	err := it.Error()
	require.ErrorIs(err, database.ErrClosed)
}

func TestValueNodeDBClear(t *testing.T) {
	require := require.New(t)
	cacheSize := 200
	baseDB := memdb.New()
	db := newValueNodeDB(
		baseDB,
		&sync.Pool{
			New: func() interface{} { return make([]byte, 0) },
		},
		&mockMetrics{},
		cacheSize,
	)

	batch := db.NewBatch()
	for _, b := range [][]byte{{1}, {2}, {3}} {
		batch.Put(ToKey(b), newNode(ToKey(b)))
	}
	require.NoError(batch.Write())

	// Assert the db is not empty
	iter := baseDB.NewIteratorWithPrefix(valueNodePrefix)
	require.True(iter.Next())
	iter.Release()

	require.NoError(db.Clear())

	iter = baseDB.NewIteratorWithPrefix(valueNodePrefix)
	defer iter.Release()
	require.False(iter.Next())
}
