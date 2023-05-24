// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package database

import (
	"bytes"
	"io"
	"testing"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"golang.org/x/exp/slices"

	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"
)

// Tests is a list of all database tests
var Tests = []func(t *testing.T, db Database){
	TestSimpleKeyValue,
	TestEmptyKey,
	TestKeyEmptyValue,
	TestSimpleKeyValueClosed,
	TestNewBatchClosed,
	TestBatchPut,
	TestBatchDelete,
	TestBatchReset,
	TestBatchReuse,
	TestBatchRewrite,
	TestBatchReplay,
	TestBatchReplayPropagateError,
	TestBatchInner,
	TestBatchLargeSize,
	TestIteratorSnapshot,
	TestIterator,
	TestIteratorStart,
	TestIteratorPrefix,
	TestIteratorStartPrefix,
	TestIteratorMemorySafety,
	TestIteratorClosed,
	TestIteratorError,
	TestIteratorErrorAfterRelease,
	TestCompactNoPanic,
	TestMemorySafetyDatabase,
	TestMemorySafetyBatch,
	TestClear,
	TestClearPrefix,
	TestModifyValueAfterPut,
	TestModifyValueAfterBatchPut,
	TestModifyValueAfterBatchPutReplay,
	TestConcurrentBatches,
	TestManySmallConcurrentKVPairBatches,
	TestPutGetEmpty,
}

var FuzzTests = []func(*testing.F, Database){
	FuzzKeyValue,
}

// TestSimpleKeyValue tests to make sure that simple Put + Get + Delete + Has
// calls return the expected values.
func TestSimpleKeyValue(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello")
	value := []byte("world")

	has, err := db.Has(key)
	require.NoError(err)
	require.False(has)

	_, err = db.Get(key)
	require.Equal(ErrNotFound, err)

	require.NoError(db.Delete(key))
	require.NoError(db.Put(key, value))

	has, err = db.Has(key)
	require.NoError(err)
	require.True(has)

	v, err := db.Get(key)
	require.NoError(err)
	require.Equal(value, v)

	require.NoError(db.Delete(key))

	has, err = db.Has(key)
	require.NoError(err)
	require.False(has)

	_, err = db.Get(key)
	require.Equal(ErrNotFound, err)

	require.NoError(db.Delete(key))
}

func TestKeyEmptyValue(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello")
	val := []byte(nil)

	_, err := db.Get(key)
	require.Equal(ErrNotFound, err)

	require.NoError(db.Put(key, val))

	value, err := db.Get(key)
	require.NoError(err)
	require.Empty(value)
}

func TestEmptyKey(t *testing.T, db Database) {
	require := require.New(t)

	var (
		nilKey   = []byte(nil)
		emptyKey = []byte{}
		val1     = []byte("hi")
		val2     = []byte("hello")
	)

	// Test that nil key can be retrieved by empty key
	_, err := db.Get(nilKey)
	require.Equal(ErrNotFound, err)

	require.NoError(db.Put(nilKey, val1))

	value, err := db.Get(emptyKey)
	require.NoError(err)
	require.Equal(value, val1)

	// Test that empty key can be retrieved by nil key
	require.NoError(db.Put(emptyKey, val2))

	value, err = db.Get(nilKey)
	require.NoError(err)
	require.Equal(value, val2)
}

// TestSimpleKeyValueClosed tests to make sure that Put + Get + Delete + Has
// calls return the correct error when the database has been closed.
func TestSimpleKeyValueClosed(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello")
	value := []byte("world")

	has, err := db.Has(key)
	require.NoError(err)
	require.False(has)

	_, err = db.Get(key)
	require.Equal(ErrNotFound, err)

	require.NoError(db.Delete(key))
	require.NoError(db.Put(key, value))

	has, err = db.Has(key)
	require.NoError(err)
	require.True(has)

	v, err := db.Get(key)
	require.NoError(err)
	require.Equal(value, v)

	require.NoError(db.Close())

	_, err = db.Has(key)
	require.Equal(ErrClosed, err)

	_, err = db.Get(key)
	require.Equal(ErrClosed, err)

	require.Equal(ErrClosed, db.Put(key, value))
	require.Equal(ErrClosed, db.Delete(key))
	require.Equal(ErrClosed, db.Close())
}

// TestMemorySafetyDatabase ensures it is safe to modify a key after passing it
// to Database.Put and Database.Get.
func TestMemorySafetyDatabase(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("1key")
	keyCopy := slices.Clone(key)
	value := []byte("value")
	key2 := []byte("2key")
	value2 := []byte("value2")

	// Put both K/V pairs in the database
	require.NoError(db.Put(key, value))
	require.NoError(db.Put(key2, value2))

	// Get the value for [key]
	gotVal, err := db.Get(key)
	require.NoError(err)
	require.Equal(value, gotVal)

	// Modify [key]; make sure the value we got before hasn't changed
	key[0] = key2[0]
	gotVal2, err := db.Get(key)
	require.NoError(err)
	require.Equal(value2, gotVal2)
	require.Equal(value, gotVal)

	// Reset [key] to its original value and make sure it's correct
	key[0] = keyCopy[0]
	gotVal, err = db.Get(key)
	require.NoError(err)
	require.Equal(value, gotVal)
}

// TestNewBatchClosed tests to make sure that calling NewBatch on a closed
// database returns a batch that errors correctly.
func TestNewBatchClosed(t *testing.T, db Database) {
	require := require.New(t)

	require.NoError(db.Close())

	batch := db.NewBatch()
	require.NotNil(batch)

	key := []byte("hello")
	value := []byte("world")

	require.NoError(batch.Put(key, value))
	require.Positive(batch.Size())
	require.Equal(ErrClosed, batch.Write())
}

// TestBatchPut tests to make sure that batched writes work as expected.
func TestBatchPut(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello")
	value := []byte("world")

	batch := db.NewBatch()
	require.NotNil(batch)

	require.NoError(batch.Put(key, value))
	require.Positive(batch.Size())
	require.NoError(batch.Write())

	has, err := db.Has(key)
	require.NoError(err)
	require.True(has)

	v, err := db.Get(key)
	require.NoError(err)
	require.Equal(value, v)

	require.NoError(db.Delete(key))

	batch = db.NewBatch()
	require.NotNil(batch)

	require.NoError(batch.Put(key, value))
	require.NoError(db.Close())
	require.Equal(ErrClosed, batch.Write())
}

// TestBatchDelete tests to make sure that batched deletes work as expected.
func TestBatchDelete(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello")
	value := []byte("world")

	require.NoError(db.Put(key, value))

	batch := db.NewBatch()
	require.NotNil(batch)

	require.NoError(batch.Delete(key))
	require.NoError(batch.Write())

	has, err := db.Has(key)
	require.NoError(err)
	require.False(has)

	_, err = db.Get(key)
	require.Equal(ErrNotFound, err)

	require.NoError(db.Delete(key))
}

// TestMemorySafetyDatabase ensures it is safe to modify a key after passing it
// to Batch.Put.
func TestMemorySafetyBatch(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello")
	keyCopy := slices.Clone(key)
	value := []byte("world")
	valueCopy := slices.Clone(value)

	batch := db.NewBatch()
	require.NotNil(batch)

	// Put a key in the batch
	require.NoError(batch.Put(key, value))
	require.Positive(batch.Size())

	// Modify the key
	key[0] = 'j'
	require.NoError(batch.Write())

	// Make sure the original key was written to the database
	has, err := db.Has(keyCopy)
	require.NoError(err)
	require.True(has)

	v, err := db.Get(keyCopy)
	require.NoError(err)
	require.Equal(valueCopy, v)

	// Make sure the new key wasn't written to the database
	has, err = db.Has(key)
	require.NoError(err)
	require.False(has)
}

// TestBatchReset tests to make sure that a batch drops un-written operations
// when it is reset.
func TestBatchReset(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello")
	value := []byte("world")

	require.NoError(db.Put(key, value))

	batch := db.NewBatch()
	require.NotNil(batch)

	require.NoError(batch.Delete(key))

	batch.Reset()

	require.Zero(batch.Size())
	require.NoError(batch.Write())

	has, err := db.Has(key)
	require.NoError(err)
	require.True(has)

	v, err := db.Get(key)
	require.NoError(err)
	require.Equal(value, v)
}

// TestBatchReuse tests to make sure that a batch can be reused once it is
// reset.
func TestBatchReuse(t *testing.T, db Database) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	batch := db.NewBatch()
	require.NotNil(batch)

	require.NoError(batch.Put(key1, value1))
	require.NoError(batch.Write())
	require.NoError(db.Delete(key1))

	has, err := db.Has(key1)
	require.NoError(err)
	require.False(has)

	batch.Reset()

	require.Zero(batch.Size())
	require.NoError(batch.Put(key2, value2))
	require.NoError(batch.Write())

	has, err = db.Has(key1)
	require.NoError(err)
	require.False(has)

	has, err = db.Has(key2)
	require.NoError(err)
	require.True(has)

	v, err := db.Get(key2)
	require.NoError(err)
	require.Equal(value2, v)
}

// TestBatchRewrite tests to make sure that write can be called multiple times
// on a batch and the values will be updated correctly.
func TestBatchRewrite(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello1")
	value := []byte("world1")

	batch := db.NewBatch()
	require.NotNil(batch)

	require.NoError(batch.Put(key, value))
	require.NoError(batch.Write())
	require.NoError(db.Delete(key))

	has, err := db.Has(key)
	require.NoError(err)
	require.False(has)

	require.NoError(batch.Write())

	has, err = db.Has(key)
	require.NoError(err)
	require.True(has)

	v, err := db.Get(key)
	require.NoError(err)
	require.Equal(value, v)
}

// TestBatchReplay tests to make sure that batches will correctly replay their
// contents.
func TestBatchReplay(t *testing.T, db Database) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	batch := db.NewBatch()
	require.NotNil(batch)

	require.NoError(batch.Put(key1, value1))
	require.NoError(batch.Put(key2, value2))
	require.NoError(batch.Delete(key1))
	require.NoError(batch.Delete(key2))
	require.NoError(batch.Put(key1, value2))

	for i := 0; i < 2; i++ {
		mockBatch := NewMockBatch(ctrl)
		gomock.InOrder(
			mockBatch.EXPECT().Put(key1, value1).Times(1),
			mockBatch.EXPECT().Put(key2, value2).Times(1),
			mockBatch.EXPECT().Delete(key1).Times(1),
			mockBatch.EXPECT().Delete(key2).Times(1),
			mockBatch.EXPECT().Put(key1, value2).Times(1),
		)

		require.NoError(batch.Replay(mockBatch))
	}
}

// TestBatchReplayPropagateError tests to make sure that batches will correctly
// propagate any returned error during Replay.
func TestBatchReplayPropagateError(t *testing.T, db Database) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	batch := db.NewBatch()
	require.NotNil(batch)

	require.NoError(batch.Put(key1, value1))
	require.NoError(batch.Put(key2, value2))

	mockBatch := NewMockBatch(ctrl)
	gomock.InOrder(
		mockBatch.EXPECT().Put(key1, value1).Return(ErrClosed).Times(1),
	)
	require.Equal(ErrClosed, batch.Replay(mockBatch))

	mockBatch = NewMockBatch(ctrl)
	gomock.InOrder(
		mockBatch.EXPECT().Put(key1, value1).Return(io.ErrClosedPipe).Times(1),
	)
	require.Equal(io.ErrClosedPipe, batch.Replay(mockBatch))
}

// TestBatchInner tests to make sure that inner can be used to write to the
// database.
func TestBatchInner(t *testing.T, db Database) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	firstBatch := db.NewBatch()
	require.NotNil(firstBatch)

	require.NoError(firstBatch.Put(key1, value1))

	secondBatch := db.NewBatch()
	require.NotNil(firstBatch)

	require.NoError(secondBatch.Put(key2, value2))

	innerFirstBatch := firstBatch.Inner()
	require.NotNil(innerFirstBatch)

	innerSecondBatch := secondBatch.Inner()
	require.NotNil(innerSecondBatch)

	require.NoError(innerFirstBatch.Replay(innerSecondBatch))
	require.NoError(innerSecondBatch.Write())

	has, err := db.Has(key1)
	require.NoError(err)
	require.True(has)

	v, err := db.Get(key1)
	require.NoError(err)
	require.Equal(value1, v)

	has, err = db.Has(key2)
	require.NoError(err)
	require.True(has)

	v, err = db.Get(key2)
	require.NoError(err)
	require.Equal(value2, v)
}

// TestBatchLargeSize tests to make sure that the batch can support a large
// amount of entries.
func TestBatchLargeSize(t *testing.T, db Database) {
	require := require.New(t)

	totalSize := 8 * units.MiB
	elementSize := 4 * units.KiB
	pairSize := 2 * elementSize // 8 KiB

	bytes := utils.RandomBytes(totalSize)

	batch := db.NewBatch()
	require.NotNil(batch)

	for len(bytes) > pairSize {
		key := bytes[:elementSize]
		bytes = bytes[elementSize:]

		value := bytes[:elementSize]
		bytes = bytes[elementSize:]

		require.NoError(batch.Put(key, value))
	}

	require.NoError(batch.Write())
}

// TestIteratorSnapshot tests to make sure the database iterates over a snapshot
// of the database at the time of the iterator creation.
func TestIteratorSnapshot(t *testing.T, db Database) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	require.NoError(db.Put(key1, value1))

	iterator := db.NewIterator()
	require.NotNil(iterator)

	defer iterator.Release()

	require.NoError(db.Put(key2, value2))
	require.True(iterator.Next())
	require.Equal(key1, iterator.Key())
	require.Equal(value1, iterator.Value())

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())
	require.NoError(iterator.Error())
}

// TestIterator tests to make sure the database iterates over the database
// contents lexicographically.
func TestIterator(t *testing.T, db Database) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

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

// TestIteratorStart tests to make sure the the iterator can be configured to
// start mid way through the database.
func TestIteratorStart(t *testing.T, db Database) {
	require := require.New(t)

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

// TestIteratorPrefix tests to make sure the iterator can be configured to skip
// keys missing the provided prefix.
func TestIteratorPrefix(t *testing.T, db Database) {
	require := require.New(t)

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

// TestIteratorStartPrefix tests to make sure that the iterator can start mid
// way through the database while skipping a prefix.
func TestIteratorStartPrefix(t *testing.T, db Database) {
	require := require.New(t)

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

// TestIteratorMemorySafety tests to make sure that keys can values are able to
// be modified from the returned iterator.
func TestIteratorMemorySafety(t *testing.T, db Database) {
	require := require.New(t)

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

	keys := [][]byte{}
	values := [][]byte{}
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

// TestIteratorClosed tests to make sure that an iterator that was created with
// a closed database will report a closed error correctly.
func TestIteratorClosed(t *testing.T, db Database) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Close())

	{
		iterator := db.NewIterator()
		require.NotNil(iterator)

		defer iterator.Release()

		require.False(iterator.Next())
		require.Nil(iterator.Key())
		require.Nil(iterator.Value())
		require.Equal(ErrClosed, iterator.Error())
	}

	{
		iterator := db.NewIteratorWithPrefix(nil)
		require.NotNil(iterator)

		defer iterator.Release()

		require.False(iterator.Next())
		require.Nil(iterator.Key())
		require.Nil(iterator.Value())
		require.Equal(ErrClosed, iterator.Error())
	}

	{
		iterator := db.NewIteratorWithStart(nil)
		require.NotNil(iterator)

		defer iterator.Release()

		require.False(iterator.Next())
		require.Nil(iterator.Key())
		require.Nil(iterator.Value())
		require.Equal(ErrClosed, iterator.Error())
	}

	{
		iterator := db.NewIteratorWithStartAndPrefix(nil, nil)
		require.NotNil(iterator)

		defer iterator.Release()

		require.False(iterator.Next())
		require.Nil(iterator.Key())
		require.Nil(iterator.Value())
		require.Equal(ErrClosed, iterator.Error())
	}
}

// TestIteratorError tests to make sure that an iterator on a database will report
// itself as being exhausted and return [ErrClosed] to indicate that the iteration
// was not successful.
// Additionally tests that an iterator that has already called Next() can still serve
// its current value after the underlying DB was closed.
func TestIteratorError(t *testing.T, db Database) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("hello2")
	value2 := []byte("world2")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))

	iterator := db.NewIterator()
	require.NotNil(iterator)

	defer iterator.Release()

	// Call Next() and ensure that if the database is closed, the iterator
	// can still report the current contents.
	require.True(iterator.Next())
	require.NoError(db.Close())
	require.Equal(key1, iterator.Key())
	require.Equal(value1, iterator.Value())

	// Subsequent calls to the iterator should return false and report an error
	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())
	require.Equal(ErrClosed, iterator.Error())
}

// TestIteratorErrorAfterRelease tests to make sure that an iterator that was
// released still reports the error correctly.
func TestIteratorErrorAfterRelease(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello1")
	value := []byte("world1")

	require.NoError(db.Put(key, value))
	require.NoError(db.Close())

	iterator := db.NewIterator()
	require.NotNil(iterator)

	iterator.Release()

	require.False(iterator.Next())
	require.Nil(iterator.Key())
	require.Nil(iterator.Value())
	require.Equal(ErrClosed, iterator.Error())
}

// TestCompactNoPanic tests to make sure compact never panics.
func TestCompactNoPanic(t *testing.T, db Database) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("z")
	value2 := []byte("world2")

	key3 := []byte("hello3")
	value3 := []byte("world3")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))
	require.NoError(db.Put(key3, value3))

	require.NoError(db.Compact(nil, nil))
	require.NoError(db.Close())
	require.Equal(ErrClosed, db.Compact(nil, nil))
}

// TestClear tests to make sure the deletion helper works as expected.
func TestClear(t *testing.T, db Database) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("z")
	value2 := []byte("world2")

	key3 := []byte("hello3")
	value3 := []byte("world3")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))
	require.NoError(db.Put(key3, value3))

	count, err := Count(db)
	require.NoError(err)
	require.Equal(3, count)

	require.NoError(Clear(db, db))

	count, err = Count(db)
	require.NoError(err)
	require.Equal(0, count)

	require.NoError(db.Close())
}

// TestClearPrefix tests to make sure prefix deletion works as expected.
func TestClearPrefix(t *testing.T, db Database) {
	require := require.New(t)

	key1 := []byte("hello1")
	value1 := []byte("world1")

	key2 := []byte("z")
	value2 := []byte("world2")

	key3 := []byte("hello3")
	value3 := []byte("world3")

	require.NoError(db.Put(key1, value1))
	require.NoError(db.Put(key2, value2))
	require.NoError(db.Put(key3, value3))

	count, err := Count(db)
	require.NoError(err)
	require.Equal(3, count)

	require.NoError(ClearPrefix(db, db, []byte("hello")))

	count, err = Count(db)
	require.NoError(err)
	require.Equal(1, count)

	has, err := db.Has(key1)
	require.NoError(err)
	require.False(has)

	has, err = db.Has(key2)
	require.NoError(err)
	require.True(has)

	has, err = db.Has(key3)
	require.NoError(err)
	require.False(has)

	require.NoError(db.Close())
}

func TestModifyValueAfterPut(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte{1}
	value := []byte{1, 2}
	originalValue := slices.Clone(value)

	require.NoError(db.Put(key, value))

	// Modify the value that was Put into the database
	// to see if the database copied the value correctly.
	value[0] = 2
	retrievedValue, err := db.Get(key)
	require.NoError(err)
	require.Equal(originalValue, retrievedValue)
}

func TestModifyValueAfterBatchPut(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte{1}
	value := []byte{1, 2}
	originalValue := slices.Clone(value)

	batch := db.NewBatch()
	require.NoError(batch.Put(key, value))

	// Modify the value that was Put into the Batch and then Write the
	// batch to the database.
	value[0] = 2
	require.NoError(batch.Write())

	// Verify that the value written to the database contains matches the original
	// value of the byte slice when Put was called.
	retrievedValue, err := db.Get(key)
	require.NoError(err)
	require.Equal(originalValue, retrievedValue)
}

func TestModifyValueAfterBatchPutReplay(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte{1}
	value := []byte{1, 2}
	originalValue := slices.Clone(value)

	batch := db.NewBatch()
	require.NoError(batch.Put(key, value))

	// Modify the value that was Put into the Batch and then Write the
	// batch to the database.
	value[0] = 2

	// Create a new batch and replay the batch onto this one before writing it to the DB.
	replayBatch := db.NewBatch()
	require.NoError(batch.Replay(replayBatch))
	require.NoError(replayBatch.Write())

	// Verify that the value written to the database contains matches the original
	// value of the byte slice when Put was called.
	retrievedValue, err := db.Get(key)
	require.NoError(err)
	require.Equal(originalValue, retrievedValue)
}

func TestConcurrentBatches(t *testing.T, db Database) {
	numBatches := 10
	keysPerBatch := 50
	keySize := 32
	valueSize := units.KiB

	require.NoError(t, runConcurrentBatches(
		db,
		numBatches,
		keysPerBatch,
		keySize,
		valueSize,
	))
}

func TestManySmallConcurrentKVPairBatches(t *testing.T, db Database) {
	numBatches := 100
	keysPerBatch := 10
	keySize := 10
	valueSize := 10

	require.NoError(t, runConcurrentBatches(
		db,
		numBatches,
		keysPerBatch,
		keySize,
		valueSize,
	))
}

func runConcurrentBatches(
	db Database,
	numBatches,
	keysPerBatch,
	keySize,
	valueSize int,
) error {
	batches := make([]Batch, 0, numBatches)
	for i := 0; i < numBatches; i++ {
		batches = append(batches, db.NewBatch())
	}

	for _, batch := range batches {
		for i := 0; i < keysPerBatch; i++ {
			key := utils.RandomBytes(keySize)
			value := utils.RandomBytes(valueSize)
			if err := batch.Put(key, value); err != nil {
				return err
			}
		}
	}

	var eg errgroup.Group
	for _, batch := range batches {
		eg.Go(batch.Write)
	}
	return eg.Wait()
}

func TestPutGetEmpty(t *testing.T, db Database) {
	require := require.New(t)

	key := []byte("hello")

	require.NoError(db.Put(key, nil))

	value, err := db.Get(key)
	require.NoError(err)
	require.Empty(value) // May be nil or empty byte slice.

	require.NoError(db.Put(key, []byte{}))

	value, err = db.Get(key)
	require.NoError(err)
	require.Empty(value) // May be nil or empty byte slice.
}

func FuzzKeyValue(f *testing.F, db Database) {
	f.Fuzz(func(t *testing.T, key []byte, value []byte) {
		require := require.New(t)

		require.NoError(db.Put(key, value))

		exists, err := db.Has(key)
		require.NoError(err)
		require.True(exists)

		gotVal, err := db.Get(key)
		require.NoError(err)
		require.True(bytes.Equal(value, gotVal))

		require.NoError(db.Delete(key))

		exists, err = db.Has(key)
		require.NoError(err)
		require.False(exists)

		_, err = db.Get(key)
		require.Equal(ErrNotFound, err)
	})
}
