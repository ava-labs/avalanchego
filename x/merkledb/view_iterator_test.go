// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

func Test_View_Iterator(t *testing.T) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	db, err := getBasicDB()
	require.NoError(err)

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))

	view, err := db.NewView(t.Context(), ViewChanges{})
	require.NoError(err)
	iterator := view.NewIterator()
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

func Test_View_Iterator_DBClosed(t *testing.T) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	db, err := getBasicDB()
	require.NoError(err)

	require.NoError(db.Put(key1, value1))

	view, err := db.NewView(t.Context(), ViewChanges{})
	require.NoError(err)
	iterator := view.NewIterator()
	require.NotNil(iterator)

	defer iterator.Release()

	require.NoError(db.Close())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())
	err = iterator.Error()
	require.ErrorIs(err, ErrInvalid)
}

// Test_View_IteratorStart tests to make sure the iterator can be configured to
// start midway through the database.
func Test_View_IteratorStart(t *testing.T) {
	require := require.New(t)
	db, err := getBasicDB()
	require.NoError(err)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))

	view, err := db.NewView(t.Context(), ViewChanges{})
	require.NoError(err)
	iterator := view.NewIteratorWithStart(key2)
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

// Test_View_IteratorPrefix tests to make sure the iterator can be configured to skip
// keys missing the provided prefix.
func Test_View_IteratorPrefix(t *testing.T) {
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

	view, err := db.NewView(t.Context(), ViewChanges{})
	require.NoError(err)
	iterator := view.NewIteratorWithPrefix([]byte("h"))
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

// Test_View_IteratorStartPrefix tests to make sure that the iterator can start
// midway through the database while skipping a prefix.
func Test_View_IteratorStartPrefix(t *testing.T) {
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

	view, err := db.NewView(t.Context(), ViewChanges{})
	require.NoError(err)
	iterator := view.NewIteratorWithStartAndPrefix(key1, []byte("h"))
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

// Test view iteration by creating a stack of views,
// inserting random key/value pairs into them, and
// iterating over the last view.
func Test_View_Iterator_Random(t *testing.T) {
	require := require.New(t)
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	rand := rand.New(rand.NewSource(now))

	var (
		numKeyChanges = 5_000
		maxKeyLen     = 16
		maxValLen     = 16
	)

	keyChanges := []KeyChange{}
	for i := 0; i < numKeyChanges; i++ {
		key := make([]byte, rand.Intn(maxKeyLen))
		_, _ = rand.Read(key)
		value := make([]byte, rand.Intn(maxValLen))
		_, _ = rand.Read(value)
		keyChanges = append(keyChanges, KeyChange{
			Key:   key,
			Value: maybe.Some(value),
		})
	}

	db, err := getBasicDB()
	require.NoError(err)

	for i := 0; i < numKeyChanges/4; i++ {
		require.NoError(db.Put(keyChanges[i].Key, keyChanges[i].Value.Value()))
	}

	ops := make([]database.BatchOp, 0, numKeyChanges/4)
	for i := numKeyChanges / 4; i < 2*numKeyChanges/4; i++ {
		ops = append(ops, database.BatchOp{Key: keyChanges[i].Key, Value: keyChanges[i].Value.Value()})
	}

	view1, err := db.NewView(t.Context(), ViewChanges{BatchOps: ops})
	require.NoError(err)

	ops = make([]database.BatchOp, 0, numKeyChanges/4)
	for i := 2 * numKeyChanges / 4; i < 3*numKeyChanges/4; i++ {
		ops = append(ops, database.BatchOp{Key: keyChanges[i].Key, Value: keyChanges[i].Value.Value()})
	}

	view2, err := view1.NewView(t.Context(), ViewChanges{BatchOps: ops})
	require.NoError(err)

	ops = make([]database.BatchOp, 0, numKeyChanges/4)
	for i := 3 * numKeyChanges / 4; i < numKeyChanges; i++ {
		ops = append(ops, database.BatchOp{Key: keyChanges[i].Key, Value: keyChanges[i].Value.Value()})
	}

	view3, err := view2.NewView(t.Context(), ViewChanges{BatchOps: ops})
	require.NoError(err)

	// Might have introduced duplicates, so only expect the latest value.
	uniqueKeyChanges := make(map[string][]byte)
	for _, keyChange := range keyChanges {
		uniqueKeyChanges[string(keyChange.Key)] = keyChange.Value.Value()
	}

	iter := view3.NewIterator()
	uniqueKeys := maps.Keys(uniqueKeyChanges)
	slices.Sort(uniqueKeys)
	i := 0
	for iter.Next() {
		expectedKey := uniqueKeys[i]
		expectedValue := uniqueKeyChanges[expectedKey]
		require.True(bytes.Equal([]byte(expectedKey), iter.Key()))
		if len(expectedValue) == 0 {
			// Don't differentiate between nil and []byte{}
			require.Empty(iter.Value())
		} else {
			require.Equal(expectedValue, iter.Value())
		}
		i++
	}
	require.Len(uniqueKeys, i)
	iter.Release()
	require.NoError(iter.Error())

	// Test with start and prefix.
	prefix := []byte{128}
	start := []byte{128, 5}
	iter = view3.NewIteratorWithStartAndPrefix(start, prefix)
	startPrefixUniqueKeys := []string{}
	// Remove keys that don't have the prefix/are before the start.
	for i := 0; i < len(uniqueKeys); i++ {
		if bytes.HasPrefix([]byte(uniqueKeys[i]), prefix) && bytes.Compare([]byte(uniqueKeys[i]), start) >= 0 {
			startPrefixUniqueKeys = append(startPrefixUniqueKeys, uniqueKeys[i])
		}
	}
	require.NotEmpty(startPrefixUniqueKeys) // Sanity check to make sure we have some keys to test.
	i = 0
	for iter.Next() {
		expectedKey := startPrefixUniqueKeys[i]
		expectedValue := uniqueKeyChanges[expectedKey]
		require.Equal([]byte(expectedKey), iter.Key())
		if len(expectedValue) == 0 {
			// Don't differentiate between nil and []byte{}
			require.Empty(iter.Value())
		} else {
			require.Equal(expectedValue, iter.Value())
		}
		i++
	}
	require.Len(startPrefixUniqueKeys, i)
	iter.Release()
	require.NoError(iter.Error())
}
