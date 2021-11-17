// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linkedhashmap

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func TestLinkedHashmap(t *testing.T) {
	assert := assert.New(t)

	lh := New()
	assert.Equal(0, lh.Len(), "a new hashmap should be empty")

	key0 := ids.GenerateTestID()
	_, exists := lh.Get(key0)
	assert.False(exists, "shouldn't have found the value")

	_, _, exists = lh.Oldest()
	assert.False(exists, "shouldn't have found a value")

	_, _, exists = lh.Newest()
	assert.False(exists, "shouldn't have found a value")

	lh.Put(key0, 0)
	assert.Equal(1, lh.Len(), "wrong hashmap length")

	val0, exists := lh.Get(key0)
	assert.True(exists, "should have found the value")
	assert.Equal(0, val0, "wrong value")

	rkey0, val0, exists := lh.Oldest()
	assert.True(exists, "should have found the value")
	assert.Equal(key0, rkey0, "wrong key")
	assert.Equal(0, val0, "wrong value")

	rkey0, val0, exists = lh.Newest()
	assert.True(exists, "should have found the value")
	assert.Equal(key0, rkey0, "wrong key")
	assert.Equal(0, val0, "wrong value")

	key1 := ids.GenerateTestID()
	lh.Put(key1, 1)
	assert.Equal(2, lh.Len(), "wrong hashmap length")

	val1, exists := lh.Get(key1)
	assert.True(exists, "should have found the value")
	assert.Equal(1, val1, "wrong value")

	rkey0, val0, exists = lh.Oldest()
	assert.True(exists, "should have found the value")
	assert.Equal(key0, rkey0, "wrong key")
	assert.Equal(0, val0, "wrong value")

	rkey1, val1, exists := lh.Newest()
	assert.True(exists, "should have found the value")
	assert.Equal(key1, rkey1, "wrong key")
	assert.Equal(1, val1, "wrong value")

	lh.Delete(key0)
	assert.Equal(1, lh.Len(), "wrong hashmap length")

	_, exists = lh.Get(key0)
	assert.False(exists, "shouldn't have found the value")

	rkey1, val1, exists = lh.Oldest()
	assert.True(exists, "should have found the value")
	assert.Equal(rkey1, key1, "wrong key")
	assert.Equal(1, val1, "wrong value")

	rkey1, val1, exists = lh.Newest()
	assert.True(exists, "should have found the value")
	assert.Equal(key1, rkey1, "wrong key")
	assert.Equal(1, val1, "wrong value")

	lh.Put(key0, 0)
	assert.Equal(2, lh.Len(), "wrong hashmap length")

	lh.Put(key1, 1)
	assert.Equal(2, lh.Len(), "wrong hashmap length")

	rkey0, val0, exists = lh.Oldest()
	assert.True(exists, "should have found the value")
	assert.Equal(key0, rkey0, "wrong key")
	assert.Equal(0, val0, "wrong value")

	rkey1, val1, exists = lh.Newest()
	assert.True(exists, "should have found the value")
	assert.Equal(key1, rkey1, "wrong key")
	assert.Equal(1, val1, "wrong value")
}

func TestIterator(t *testing.T) {
	assert := assert.New(t)
	id1, id2, id3 := ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID()

	// Case: No elements
	{
		lh := New()
		iter := lh.NewIterator()
		assert.NotNil(iter)
		// Should immediately be exhausted
		assert.False(iter.Next())
		assert.False(iter.Next())
		// Should be empty
		assert.EqualValues(ids.Empty, iter.Key())
		assert.Nil(iter.Value())
	}

	// Case: 1 element
	{
		lh := New()
		iter := lh.NewIterator()
		assert.NotNil(iter)
		lh.Put(id1, 1)
		assert.True(iter.Next())
		assert.EqualValues(id1, iter.Key())
		assert.EqualValues(1, iter.Value())
		// Should be empty
		assert.False(iter.Next())
		// Re-assign id1 --> 10
		lh.Put(id1, 10)
		iter = lh.NewIterator() // New iterator
		assert.True(iter.Next())
		assert.EqualValues(id1, iter.Key())
		assert.EqualValues(10, iter.Value())
		// Should be empty
		assert.False(iter.Next())
		// Delete id1
		lh.Delete(id1)
		iter = lh.NewIterator()
		assert.NotNil(iter)
		// Should immediately be exhausted
		assert.False(iter.Next())
	}

	// Case: Multiple elements
	{
		lh := New()
		lh.Put(id1, 1)
		lh.Put(id2, 2)
		lh.Put(id3, 3)
		iter := lh.NewIterator()
		// Should give back all 3 elements
		assert.True(iter.Next())
		assert.EqualValues(id1, iter.Key())
		assert.EqualValues(1, iter.Value())
		assert.True(iter.Next())
		assert.EqualValues(id2, iter.Key())
		assert.EqualValues(2, iter.Value())
		assert.True(iter.Next())
		assert.EqualValues(id3, iter.Key())
		assert.EqualValues(3, iter.Value())
		// Should be exhausted
		assert.False(iter.Next())
	}

	// Case: Delete element that has been iterated over
	{
		lh := New()
		lh.Put(id1, 1)
		lh.Put(id2, 2)
		lh.Put(id3, 3)
		iter := lh.NewIterator()
		assert.True(iter.Next())
		assert.True(iter.Next())
		lh.Delete(id1)
		lh.Delete(id2)
		assert.True(iter.Next())
		assert.EqualValues(id3, iter.Key())
		assert.EqualValues(3, iter.Value())
		// Should be exhausted
		assert.False(iter.Next())
	}
}
