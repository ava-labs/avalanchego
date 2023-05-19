// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_TrieView_Iterator(t *testing.T) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	db, err := getBasicDB()
	require.NoError(err)

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))

	iterator := db.NewIterator()
	require.NotNil(iterator)

	defer iterator.Release()

	require.True(iterator.Next())
	require.Equal(key1, iterator.Key())
	require.Equal(value1, iterator.Value())

	require.True(iterator.Next())
	require.Equal(key2, iterator.Key())
	require.Equal(value2, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())
	require.NoError(iterator.Error())
}

// Test_TrieView_IteratorStart tests to make sure the iterator can be configured to
// start midway through the database.
func Test_TrieView_IteratorStart(t *testing.T) {
	require := require.New(t)
	db, err := getBasicDB()
	require.NoError(err)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))

	iterator := db.NewIteratorWithStart(key2)
	require.NotNil(iterator)

	defer iterator.Release()

	require.True(iterator.Next())
	require.Equal(key2, iterator.Key())
	require.Equal(value2, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())
	require.NoError(iterator.Error())
}

// Test_TrieView_IteratorPrefix tests to make sure the iterator can be configured to skip
// keys missing the provided prefix.
func Test_TrieView_IteratorPrefix(t *testing.T) {
	require := require.New(t)
	db, err := getBasicDB()
	require.NoError(err)

	key1 := []byte("hello")
	value1 := []byte("world1")

	key2 := []byte("goodbye")
	value2 := []byte("world2")

	key3 := []byte("joy")
	value3 := []byte("world3")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))
	require.NoError(db.Put(key3, value3))

	iterator := db.NewIteratorWithPrefix([]byte("h"))
	require.NotNil(iterator)

	defer iterator.Release()

	require.True(iterator.Next())
	require.Equal(key1, iterator.Key())
	require.Equal(value1, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())
	require.NoError(iterator.Error())
}

// Test_TrieView_IteratorStartPrefix tests to make sure that the iterator can start
// midway through the database while skipping a prefix.
func Test_TrieView_IteratorStartPrefix(t *testing.T) {
	require := require.New(t)
	db, err := getBasicDB()
	require.NoError(err)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("z")
	value2 := []byte("world2")

	key3 := []byte("hello3")
	value3 := []byte("world3")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))
	require.NoError(db.Put(key3, value3))

	iterator := db.NewIteratorWithStartAndPrefix(key1, []byte("h"))
	require.NotNil(iterator)

	defer iterator.Release()

	require.True(iterator.Next())
	require.Equal(key1, iterator.Key())
	require.Equal(value1, iterator.Value())

	require.True(iterator.Next())
	require.Equal(key3, iterator.Key())
	require.Equal(value3, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())
	require.NoError(iterator.Error())
}

// Test_TrieView_IteratorMemorySafety tests to make sure that keys and values can
// be modified from the returned iterator.
func Test_TrieView_IteratorMemorySafety(t *testing.T) {
	require := require.New(t)
	db, err := getBasicDB()
	require.NoError(err)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("z")
	value2 := []byte("world2")

	key3 := []byte("hello3")
	value3 := []byte("world3")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))
	require.NoError(db.Put(key3, value3))

	iterator := db.NewIterator()
	require.NotNil(iterator)

	defer iterator.Release()

	var keys [][]byte
	var values [][]byte
	for iterator.Next() {
		keys = append(keys, iterator.Key())
		values = append(values, iterator.Value())
	}

	expectedKeys := [][]byte{
		key1,
		key3,
		key2,
	}
	expectedValues := [][]byte{
		value1,
		value3,
		value2,
	}

	for i, key := range keys {
		value := values[i]
		expectedKey := expectedKeys[i]
		expectedValue := expectedValues[i]

		require.Equal(expectedKey, key)
		require.Equal(expectedValue, value)
	}
}
