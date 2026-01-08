// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func Test_Metrics_Basic_Usage(t *testing.T) {
	config := NewConfig()
	// Set to nil so that we use a mockMetrics instead of the real one inside
	// merkledb.
	config.Reg = nil

	db, err := newDB(
		t.Context(),
		memdb.New(),
		config,
	)
	require.NoError(t, err)

	db.metrics.(*mockMetrics).nodeReadCount = 0
	db.metrics.(*mockMetrics).nodeWriteCount = 0
	db.metrics.(*mockMetrics).hashCount = 0

	require.NoError(t, db.Put([]byte("key"), []byte("value")))

	require.Equal(t, int64(1), db.metrics.(*mockMetrics).nodeReadCount)
	require.Equal(t, int64(1), db.metrics.(*mockMetrics).nodeWriteCount)
	require.Equal(t, int64(1), db.metrics.(*mockMetrics).hashCount)

	require.NoError(t, db.Delete([]byte("key")))

	require.Equal(t, int64(1), db.metrics.(*mockMetrics).nodeReadCount)
	require.Equal(t, int64(2), db.metrics.(*mockMetrics).nodeWriteCount)
	require.Equal(t, int64(1), db.metrics.(*mockMetrics).hashCount)

	_, err = db.Get([]byte("key2"))
	require.ErrorIs(t, err, database.ErrNotFound)

	require.Equal(t, int64(2), db.metrics.(*mockMetrics).nodeReadCount)
	require.Equal(t, int64(2), db.metrics.(*mockMetrics).nodeWriteCount)
	require.Equal(t, int64(1), db.metrics.(*mockMetrics).hashCount)
}

func Test_Metrics_Initialize(t *testing.T) {
	db, err := New(
		t.Context(),
		memdb.New(),
		NewConfig(),
	)
	require.NoError(t, err)

	require.NoError(t, db.Put([]byte("key"), []byte("value")))

	val, err := db.Get([]byte("key"))
	require.NoError(t, err)
	require.Equal(t, []byte("value"), val)

	require.NoError(t, db.Delete([]byte("key")))
}
