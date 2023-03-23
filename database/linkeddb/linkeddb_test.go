// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package linkeddb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestLinkedDB(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key := []byte("hello")
	value := []byte("world")

	has, err := ldb.Has(key)
	require.NoError(err)
	require.False(has, "db unexpectedly had key %s", key)

	_, err = ldb.Get(key)
	require.Equal(database.ErrNotFound, err, "Expected db.Get to return a Not Found error.")

	err = ldb.Delete(key)
	require.NoError(err)

	err = ldb.Put(key, value)
	require.NoError(err)

	has, err = ldb.Has(key)
	require.NoError(err)
	require.True(has, "db should have had key %s", key)

	v, err := ldb.Get(key)
	require.NoError(err)
	require.Equal(value, v)

	err = ldb.Delete(key)
	require.NoError(err)

	has, err = ldb.Has(key)
	require.NoError(err)
	require.False(has, "db unexpectedly had key %s", key)

	_, err = ldb.Get(key)
	require.Equal(database.ErrNotFound, err, "Expected db.Get to return a Not Found error.")

	iterator := db.NewIterator()
	next := iterator.Next()
	require.False(next, "database should be empty")
	iterator.Release()
}

func TestLinkedDBDuplicatedPut(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key := []byte("hello")
	value1 := []byte("world1")
	value2 := []byte("world2")

	err := ldb.Put(key, value1)
	require.NoError(err)

	err = ldb.Put(key, value2)
	require.NoError(err)

	v, err := ldb.Get(key)
	require.NoError(err)
	require.Equal(value2, v)

	err = ldb.Delete(key)
	require.NoError(err)

	iterator := db.NewIterator()
	next := iterator.Next()
	require.False(next, "database should be empty")
	iterator.Release()
}

func TestLinkedDBMultiplePuts(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key1 := []byte("hello1")
	key2 := []byte("hello2")
	key3 := []byte("hello3")
	value1 := []byte("world1")
	value2 := []byte("world2")
	value3 := []byte("world3")

	err := ldb.Put(key1, value1)
	require.NoError(err)

	err = ldb.Put(key2, value2)
	require.NoError(err)

	v, err := ldb.Get(key1)
	require.NoError(err)
	require.Equal(value1, v)

	v, err = ldb.Get(key2)
	require.NoError(err)
	require.Equal(value2, v)

	err = ldb.Delete(key2)
	require.NoError(err)

	err = ldb.Put(key2, value2)
	require.NoError(err)

	err = ldb.Put(key3, value3)
	require.NoError(err)

	err = ldb.Delete(key2)
	require.NoError(err)

	err = ldb.Delete(key1)
	require.NoError(err)

	err = ldb.Delete(key3)
	require.NoError(err)

	iterator := db.NewIterator()
	next := iterator.Next()
	require.False(next, "database should be empty")
	iterator.Release()
}

func TestEmptyLinkedDBIterator(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	iterator := ldb.NewIterator()
	next := iterator.Next()
	require.False(next, "The iterator should now be exhausted")

	k := iterator.Key()
	require.Nil(k, "The iterator returned the wrong key")

	v := iterator.Value()
	require.Nil(v, "The iterator returned the wrong value")

	err := iterator.Error()
	require.NoError(err)

	iterator.Release()
}

func TestLinkedDBLoadHeadKey(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key := []byte("hello")
	value := []byte("world")

	err := ldb.Put(key, value)
	require.NoError(err)

	ldb = NewDefault(db)

	iterator := ldb.NewIterator()
	next := iterator.Next()
	require.True(next, "The iterator shouldn't be exhausted yet")

	k := iterator.Key()
	require.Equal(key, k, "The iterator returned the wrong key")

	v := iterator.Value()
	require.Equal(value, v, "The iterator returned the wrong value")

	next = iterator.Next()
	require.False(next, "The iterator should now be exhausted")

	k = iterator.Key()
	require.Nil(k, "The iterator returned the wrong key")

	v = iterator.Value()
	require.Nil(v, "The iterator returned the wrong value")

	err = iterator.Error()
	require.NoError(err)

	iterator.Release()
}

func TestSingleLinkedDBIterator(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key := []byte("hello")
	value := []byte("world")

	err := ldb.Put(key, value)
	require.NoError(err)

	iterator := ldb.NewIterator()
	next := iterator.Next()
	require.True(next, "The iterator shouldn't be exhausted yet")

	k := iterator.Key()
	require.Equal(key, k, "The iterator returned the wrong key")

	v := iterator.Value()
	require.Equal(value, v, "The iterator returned the wrong value")

	next = iterator.Next()
	require.False(next, "The iterator should now be exhausted")

	k = iterator.Key()
	require.Nil(k, "The iterator returned the wrong key")

	v = iterator.Value()
	require.Nil(v, "The iterator returned the wrong value")

	err = iterator.Error()
	require.NoError(err)

	iterator.Release()
}

func TestMultipleLinkedDBIterator(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key0 := []byte("hello0")
	key1 := []byte("hello1")
	value0 := []byte("world0")
	value1 := []byte("world1")

	err := ldb.Put(key0, value0)
	require.NoError(err)

	err = ldb.Put(key1, value1)
	require.NoError(err)

	iterator := ldb.NewIterator()
	next := iterator.Next()
	require.True(next, "The iterator shouldn't be exhausted yet")

	k := iterator.Key()
	require.Equal(key1, k, "The iterator returned the wrong key")

	v := iterator.Value()
	require.Equal(value1, v, "The iterator returned the wrong value")

	next = iterator.Next()
	require.True(next, "The iterator shouldn't be exhausted yet")

	k = iterator.Key()
	require.Equal(key0, k, "The iterator returned the wrong key")

	v = iterator.Value()
	require.Equal(value0, v, "The iterator returned the wrong value")

	next = iterator.Next()
	require.False(next, "The iterator should now be exhausted")

	err = iterator.Error()
	require.NoError(err)

	iterator.Release()
}

func TestMultipleLinkedDBIteratorStart(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key0 := []byte("hello0")
	key1 := []byte("hello1")
	value0 := []byte("world0")
	value1 := []byte("world1")

	err := ldb.Put(key0, value0)
	require.NoError(err)

	err = ldb.Put(key1, value1)
	require.NoError(err)

	iterator := ldb.NewIteratorWithStart(key1)
	next := iterator.Next()
	require.True(next, "The iterator shouldn't be exhausted yet")

	k := iterator.Key()
	require.Equal(key1, k, "The iterator returned the wrong key")

	v := iterator.Value()
	require.Equal(value1, v, "The iterator returned the wrong value")

	next = iterator.Next()
	require.True(next, "The iterator shouldn't be exhausted yet")

	k = iterator.Key()
	require.Equal(key0, k, "The iterator returned the wrong key")

	v = iterator.Value()
	require.Equal(value0, v, "The iterator returned the wrong value")

	next = iterator.Next()
	require.False(next, "The iterator should now be exhausted")

	err = iterator.Error()
	require.NoError(err)

	iterator.Release()
}

func TestSingleLinkedDBIteratorStart(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key0 := []byte("hello0")
	key1 := []byte("hello1")
	value0 := []byte("world0")
	value1 := []byte("world1")

	err := ldb.Put(key0, value0)
	require.NoError(err)

	err = ldb.Put(key1, value1)
	require.NoError(err)

	iterator := ldb.NewIteratorWithStart(key0)

	next := iterator.Next()
	require.True(next, "The iterator shouldn't be exhausted yet")

	k := iterator.Key()
	require.Equal(key0, k, "The iterator returned the wrong key")

	v := iterator.Value()
	require.Equal(value0, v, "The iterator returned the wrong value")

	next = iterator.Next()
	require.False(next, "The iterator should now be exhausted")

	err = iterator.Error()
	require.NoError(err)

	iterator.Release()
}

// Test that if we call NewIteratorWithStart with a key that is not
// in the database, the iterator will start at the head
func TestEmptyLinkedDBIteratorStart(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key0 := []byte("hello0")
	key1 := []byte("hello1")
	key2 := []byte("hello2")
	value0 := []byte("world0")
	value1 := []byte("world1")

	err := ldb.Put(key0, value0)
	require.NoError(err)

	err = ldb.Put(key1, value1)
	require.NoError(err)

	iter := ldb.NewIteratorWithStart(key2)

	i := 0
	for iter.Next() {
		require.Contains([][]byte{key0, key1}, iter.Key())
		require.Contains([][]byte{value0, value1}, iter.Value())
		i++
	}
	require.Equal(2, i)

	err = iter.Error()
	require.NoError(err)

	iter.Release()
}

func TestLinkedDBIsEmpty(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	isEmpty, err := ldb.IsEmpty()
	require.NoError(err)
	require.True(isEmpty)

	key := []byte("hello")
	value := []byte("world")

	err = ldb.Put(key, value)
	require.NoError(err)

	isEmpty, err = ldb.IsEmpty()
	require.NoError(err)
	require.False(isEmpty)

	err = ldb.Delete(key)
	require.NoError(err)

	isEmpty, err = ldb.IsEmpty()
	require.NoError(err)
	require.True(isEmpty)
}

func TestLinkedDBHeadKey(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	_, err := ldb.HeadKey()
	require.Equal(database.ErrNotFound, err)

	key0 := []byte("hello0")
	value0 := []byte("world0")
	key1 := []byte("hello1")
	value1 := []byte("world1")

	err = ldb.Put(key0, value0)
	require.NoError(err)

	headKey, err := ldb.HeadKey()
	require.NoError(err)
	require.Equal(key0, headKey)

	err = ldb.Put(key1, value1)
	require.NoError(err)

	headKey, err = ldb.HeadKey()
	require.NoError(err)
	require.Equal(key1, headKey)

	err = ldb.Delete(key1)
	require.NoError(err)

	headKey, err = ldb.HeadKey()
	require.NoError(err)
	require.Equal(key0, headKey)
}

func TestLinkedDBHead(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	_, _, err := ldb.Head()
	require.Equal(database.ErrNotFound, err)

	key0 := []byte("hello0")
	value0 := []byte("world0")
	key1 := []byte("hello1")
	value1 := []byte("world1")

	err = ldb.Put(key0, value0)
	require.NoError(err)

	headKey, headVal, err := ldb.Head()
	require.NoError(err)
	require.Equal(key0, headKey)
	require.Equal(value0, headVal)

	err = ldb.Put(key1, value1)
	require.NoError(err)

	headKey, headVal, err = ldb.Head()
	require.NoError(err)
	require.Equal(key1, headKey)
	require.Equal(value1, headVal)

	err = ldb.Delete(key1)
	require.NoError(err)

	headKey, headVal, err = ldb.Head()
	require.NoError(err)
	require.Equal(key0, headKey)
	require.Equal(value0, headVal)
}
