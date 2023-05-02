// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func Test_Metrics_Basic_Usage(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 300,
			NodeCacheSize: minCacheSize,
		},
	)
	require.NoError(err)

	require.NoError(db.Put([]byte("key"), []byte("value")))

	require.Equal(int64(1), db.metrics.(*mockMetrics).keyReadCount)
	require.Equal(int64(1), db.metrics.(*mockMetrics).keyWriteCount)
	require.Equal(int64(3), db.metrics.(*mockMetrics).hashCount)

	require.NoError(db.Delete([]byte("key")))

	require.Equal(int64(1), db.metrics.(*mockMetrics).keyReadCount)
	require.Equal(int64(2), db.metrics.(*mockMetrics).keyWriteCount)
	require.Equal(int64(4), db.metrics.(*mockMetrics).hashCount)

	_, err = db.Get([]byte("key2"))
	require.ErrorIs(err, database.ErrNotFound)

	require.Equal(int64(2), db.metrics.(*mockMetrics).keyReadCount)
	require.Equal(int64(2), db.metrics.(*mockMetrics).keyWriteCount)
	require.Equal(int64(4), db.metrics.(*mockMetrics).hashCount)
}

func Test_Metrics_Initialize(t *testing.T) {
	require := require.New(t)

	db, err := New(
		context.Background(),
		memdb.New(),
		Config{
			Tracer:        newNoopTracer(),
			HistoryLength: 300,
			Reg:           prometheus.NewRegistry(),
			NodeCacheSize: 1000,
		},
	)
	require.NoError(err)

	require.NoError(db.Put([]byte("key"), []byte("value")))

	val, err := db.Get([]byte("key"))
	require.NoError(err)
	require.Equal([]byte("value"), val)

	require.NoError(db.Delete([]byte("key")))
}
