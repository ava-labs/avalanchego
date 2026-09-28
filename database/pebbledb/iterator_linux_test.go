// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build cgo

package pebbledb

import (
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/units"
)

// With cgo, pebble allocates blocks with C malloc. The value's block is larger
// than a cache shard, so the iterator holds its only reference, and it is above
// glibc's mmap threshold, so freeing it unmaps the memory. Reading the last
// value after the iterator is exhausted then faults instead of reading garbage.
func TestIteratorReleaseAfterLargeLastValue(t *testing.T) {
	require := require.New(t)

	cfg := DefaultConfig
	cfg.CacheSize = units.MiB
	cfgBytes, err := json.Marshal(cfg)
	require.NoError(err)

	db, err := New(t.TempDir(), cfgBytes, logging.NoLog{}, prometheus.NewRegistry())
	require.NoError(err)
	defer db.Close()

	require.NoError(db.Put([]byte("key"), make([]byte, 64*units.MiB)))
	// Read the value from an sstable block rather than from the memtable.
	require.NoError(db.(*Database).pebbleDB.Flush())

	it := db.NewIterator()
	require.True(it.Next())
	require.False(it.Next())
	it.Release()
	require.NoError(it.Error())
}
