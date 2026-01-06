// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	require.False(has)

	_, err = ldb.Get(key)
	require.Equal(database.ErrNotFound, err)

	require.NoError(ldb.Delete(key))

	require.NoError(ldb.Put(key, value))

	has, err = ldb.Has(key)
	require.NoError(err)
	require.True(has)

	v, err := ldb.Get(key)
	require.NoError(err)
	require.Equal(value, v)

	require.NoError(ldb.Delete(key))

	has, err = ldb.Has(key)
	require.NoError(err)
	require.False(has)

	_, err = ldb.Get(key)
	require.Equal(database.ErrNotFound, err)

	iterator := db.NewIterator()
	require.False(iterator.Next())
	iterator.Release()
}

func TestLinkedDBDuplicatedPut(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key := []byte("hello")
	value1 := []byte("world1")
	value2 := []byte("world2")

	require.NoError(ldb.Put(key, value1))

	require.NoError(ldb.Put(key, value2))

	v, err := ldb.Get(key)
	require.NoError(err)
	require.Equal(value2, v)

	require.NoError(ldb.Delete(key))

	iterator := db.NewIterator()
	require.False(iterator.Next())
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

	require.NoError(ldb.Put(key1, value1))

	require.NoError(ldb.Put(key2, value2))

	v, err := ldb.Get(key1)
	require.NoError(err)
	require.Equal(value1, v)

	v, err = ldb.Get(key2)
	require.NoError(err)
	require.Equal(value2, v)

	require.NoError(ldb.Delete(key2))

	require.NoError(ldb.Put(key2, value2))

	require.NoError(ldb.Put(key3, value3))

	require.NoError(ldb.Delete(key2))

	require.NoError(ldb.Delete(key1))

	require.NoError(ldb.Delete(key3))

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

	require.NoError(iterator.Error())

	iterator.Release()
}

func TestLinkedDBLoadHeadKey(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key := []byte("hello")
	value := []byte("world")

	require.NoError(ldb.Put(key, value))

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

	require.NoError(iterator.Error())

	iterator.Release()
}

func TestSingleLinkedDBIterator(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	ldb := NewDefault(db)

	key := []byte("hello")
	value := []byte("world")

	require.NoError(ldb.Put(key, value))

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

	require.NoError(iterator.Error())

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

	require.NoError(ldb.Put(key0, value0))

	require.NoError(ldb.Put(key1, value1))

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

	require.NoError(iterator.Error())

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

	require.NoError(ldb.Put(key0, value0))

	require.NoError(ldb.Put(key1, value1))

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

	require.NoError(iterator.Error())

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

	require.NoError(ldb.Put(key0, value0))

	require.NoError(ldb.Put(key1, value1))

	iterator := ldb.NewIteratorWithStart(key0)

	next := iterator.Next()
	require.True(next, "The iterator shouldn't be exhausted yet")

	k := iterator.Key()
	require.Equal(key0, k, "The iterator returned the wrong key")

	v := iterator.Value()
	require.Equal(value0, v, "The iterator returned the wrong value")

	next = iterator.Next()
	require.False(next, "The iterator should now be exhausted")

	require.NoError(iterator.Error())

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

	require.NoError(ldb.Put(key0, value0))

	require.NoError(ldb.Put(key1, value1))

	iter := ldb.NewIteratorWithStart(key2)

	i := 0
	for iter.Next() {
		require.Contains([][]byte{key0, key1}, iter.Key())
		require.Contains([][]byte{value0, value1}, iter.Value())
		i++
	}
	require.Equal(2, i)

	require.NoError(iter.Error())

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

	require.NoError(ldb.Put(key, value))

	isEmpty, err = ldb.IsEmpty()
	require.NoError(err)
	require.False(isEmpty)

	require.NoError(ldb.Delete(key))

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

	require.NoError(ldb.Put(key0, value0))

	headKey, err := ldb.HeadKey()
	require.NoError(err)
	require.Equal(key0, headKey)

	require.NoError(ldb.Put(key1, value1))

	headKey, err = ldb.HeadKey()
	require.NoError(err)
	require.Equal(key1, headKey)

	require.NoError(ldb.Delete(key1))

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

	require.NoError(ldb.Put(key0, value0))

	headKey, headVal, err := ldb.Head()
	require.NoError(err)
	require.Equal(key0, headKey)
	require.Equal(value0, headVal)

	require.NoError(ldb.Put(key1, value1))

	headKey, headVal, err = ldb.Head()
	require.NoError(err)
	require.Equal(key1, headKey)
	require.Equal(value1, headVal)

	require.NoError(ldb.Delete(key1))

	headKey, headVal, err = ldb.Head()
	require.NoError(err)
	require.Equal(key0, headKey)
	require.Equal(value0, headVal)
}
