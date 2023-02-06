// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/database/memdb"
)

func TestBatch_Put(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  1,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)

	bIntf := db.NewBatch()
	b, ok := bIntf.(*batch)
	require.True(ok)

	// Put a KV pair
	key, value := []byte("key"), []byte("value")
	err = b.Put(key, value)
	require.NoError(err)
	require.Equal(len(key)+len(value), b.size)
	gotVal, ok := b.data.Get(string(key))
	require.True(ok)
	require.Equal(value, gotVal.value)

	// Make sure we're copying the key
	originalKey := slices.Clone(key)
	key[0] = byte('a')
	_, ok = b.data.Get(string(key))
	require.False(ok)
	key = originalKey

	// Make sure we're copying the value
	originalValue := slices.Clone(gotVal.value)
	value[0]++
	require.Equal(originalValue, gotVal.value)

	// Overwrite the value
	value = []byte("value2")
	err = b.Put(key, value)
	require.NoError(err)

	gotVal, ok = b.data.Get(string(key))
	require.True(ok)
	require.Equal(value, gotVal.value)
	require.False(gotVal.delete)
	require.Equal(len(key)+len(value), b.size)
}

func TestBatch_Delete(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  1,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)

	bIntf := db.NewBatch()
	b, ok := bIntf.(*batch)
	require.True(ok)

	key, value := []byte("key"), []byte("value")

	err = b.Put(key, value)
	require.NoError(err)

	err = b.Delete(key)
	require.NoError(err)

	gotVal, ok := b.data.Get(string(key))
	require.True(ok)
	require.True(gotVal.delete)
	require.Equal(len(key), b.size)
	require.Equal(1, b.data.Len())
}

func TestBatch_Reset(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:         newNoopTracer(),
			HistoryLength:  1,
			ValueCacheSize: minCacheSize,
			NodeCacheSize:  minCacheSize,
		},
	)
	require.NoError(err)

	bIntf := db.NewBatch()
	b, ok := bIntf.(*batch)
	require.True(ok)

	key, value := []byte("key"), []byte("value")

	err = b.Put(key, value)
	require.NoError(err)

	b.Reset()

	require.Equal(0, b.data.Len())
	require.Equal(0, b.size)
}
