// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pebbledb

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// Note: TestInterface tests other batch functionality.
func TestBatch(t *testing.T) {
	require := require.New(t)
	dirName := t.TempDir()

	db, err := New(dirName, nil, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)

	batchIntf := db.NewBatch()
	batch, ok := batchIntf.(*batch)
	require.True(ok)

	require.False(batch.written)

	key1, value1 := []byte("key1"), []byte("value1")
	require.NoError(batch.Put(key1, value1))
	require.Equal(len(key1)+len(value1)+pebbleByteOverHead, batch.Size())

	require.NoError(batch.Write())

	require.True(batch.written)

	got, err := db.Get(key1)
	require.NoError(err)
	require.Equal(value1, got)

	batch.Reset()
	require.False(batch.written)
	require.Zero(batch.Size())

	require.NoError(db.Close())
}
