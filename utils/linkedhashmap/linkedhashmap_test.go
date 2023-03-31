// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linkedhashmap

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestLinkedHashmap(t *testing.T) {
	require := require.New(t)

	lh := New[ids.ID, int]()
	require.Equal(0, lh.Len(), "a new hashmap should be empty")

	key0 := ids.GenerateTestID()
	_, exists := lh.Get(key0)
	require.False(exists, "shouldn't have found the value")

	_, _, exists = lh.Oldest()
	require.False(exists, "shouldn't have found a value")

	_, _, exists = lh.Newest()
	require.False(exists, "shouldn't have found a value")

	lh.Put(key0, 0)
	require.Equal(1, lh.Len(), "wrong hashmap length")

	val0, exists := lh.Get(key0)
	require.True(exists, "should have found the value")
	require.Equal(0, val0, "wrong value")

	rkey0, val0, exists := lh.Oldest()
	require.True(exists, "should have found the value")
	require.Equal(key0, rkey0, "wrong key")
	require.Equal(0, val0, "wrong value")

	rkey0, val0, exists = lh.Newest()
	require.True(exists, "should have found the value")
	require.Equal(key0, rkey0, "wrong key")
	require.Equal(0, val0, "wrong value")

	key1 := ids.GenerateTestID()
	lh.Put(key1, 1)
	require.Equal(2, lh.Len(), "wrong hashmap length")

	val1, exists := lh.Get(key1)
	require.True(exists, "should have found the value")
	require.Equal(1, val1, "wrong value")

	rkey0, val0, exists = lh.Oldest()
	require.True(exists, "should have found the value")
	require.Equal(key0, rkey0, "wrong key")
	require.Equal(0, val0, "wrong value")

	rkey1, val1, exists := lh.Newest()
	require.True(exists, "should have found the value")
	require.Equal(key1, rkey1, "wrong key")
	require.Equal(1, val1, "wrong value")

	lh.Delete(key0)
	require.Equal(1, lh.Len(), "wrong hashmap length")

	_, exists = lh.Get(key0)
	require.False(exists, "shouldn't have found the value")

	rkey1, val1, exists = lh.Oldest()
	require.True(exists, "should have found the value")
	require.Equal(rkey1, key1, "wrong key")
	require.Equal(1, val1, "wrong value")

	rkey1, val1, exists = lh.Newest()
	require.True(exists, "should have found the value")
	require.Equal(key1, rkey1, "wrong key")
	require.Equal(1, val1, "wrong value")

	lh.Put(key0, 0)
	require.Equal(2, lh.Len(), "wrong hashmap length")

	lh.Put(key1, 1)
	require.Equal(2, lh.Len(), "wrong hashmap length")

	rkey0, val0, exists = lh.Oldest()
	require.True(exists, "should have found the value")
	require.Equal(key0, rkey0, "wrong key")
	require.Equal(0, val0, "wrong value")

	rkey1, val1, exists = lh.Newest()
	require.True(exists, "should have found the value")
	require.Equal(key1, rkey1, "wrong key")
	require.Equal(1, val1, "wrong value")
}

func TestIterator(t *testing.T) {
	require := require.New(t)
	id1, id2, id3 := ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID()

	// Case: No elements
	{
		lh := New[ids.ID, int]()
		iter := lh.NewIterator()
		require.NotNil(iter)
		// Should immediately be exhausted
		require.False(iter.Next())
		require.False(iter.Next())
		// Should be empty
		require.EqualValues(ids.Empty, iter.Key())
		require.Zero(iter.Value())
	}

	// Case: 1 element
	{
		lh := New[ids.ID, int]()
		iter := lh.NewIterator()
		require.NotNil(iter)
		lh.Put(id1, 1)
		require.True(iter.Next())
		require.EqualValues(id1, iter.Key())
		require.EqualValues(1, iter.Value())
		// Should be empty
		require.False(iter.Next())
		// Re-assign id1 --> 10
		lh.Put(id1, 10)
		iter = lh.NewIterator() // New iterator
		require.True(iter.Next())
		require.EqualValues(id1, iter.Key())
		require.EqualValues(10, iter.Value())
		// Should be empty
		require.False(iter.Next())
		// Delete id1
		lh.Delete(id1)
		iter = lh.NewIterator()
		require.NotNil(iter)
		// Should immediately be exhausted
		require.False(iter.Next())
	}

	// Case: Multiple elements
	{
		lh := New[ids.ID, int]()
		lh.Put(id1, 1)
		lh.Put(id2, 2)
		lh.Put(id3, 3)
		iter := lh.NewIterator()
		// Should give back all 3 elements
		require.True(iter.Next())
		require.EqualValues(id1, iter.Key())
		require.EqualValues(1, iter.Value())
		require.True(iter.Next())
		require.EqualValues(id2, iter.Key())
		require.EqualValues(2, iter.Value())
		require.True(iter.Next())
		require.EqualValues(id3, iter.Key())
		require.EqualValues(3, iter.Value())
		// Should be exhausted
		require.False(iter.Next())
	}

	// Case: Delete element that has been iterated over
	{
		lh := New[ids.ID, int]()
		lh.Put(id1, 1)
		lh.Put(id2, 2)
		lh.Put(id3, 3)
		iter := lh.NewIterator()
		require.True(iter.Next())
		require.True(iter.Next())
		lh.Delete(id1)
		lh.Delete(id2)
		require.True(iter.Next())
		require.EqualValues(id3, iter.Key())
		require.EqualValues(3, iter.Value())
		// Should be exhausted
		require.False(iter.Next())
	}
}
