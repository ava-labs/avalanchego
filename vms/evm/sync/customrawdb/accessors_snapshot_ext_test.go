// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"
)

func TestSnapshotBlockHashReadWriteDelete(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Initially empty
	got := ReadSnapshotBlockHash(db)
	require.Equal(t, common.Hash{}, got)

	// Write and read back
	want := common.HexToHash("0xdeadbeef")
	WriteSnapshotBlockHash(db, want)
	got = ReadSnapshotBlockHash(db)
	require.Equal(t, want, got)

	// Delete and verify empty
	DeleteSnapshotBlockHash(db)
	got = ReadSnapshotBlockHash(db)
	require.Equal(t, common.Hash{}, got)
}

func TestNewAccountSnapshotsIterator(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Keys that match and don't match the iterator length filter
	a1 := common.HexToHash("0x01")
	a2 := common.HexToHash("0x02")
	key1 := append(append([]byte{}, rawdb.SnapshotAccountPrefix...), a1.Bytes()...)
	key2 := append(append([]byte{}, rawdb.SnapshotAccountPrefix...), a2.Bytes()...)
	// Non-matching: extra byte appended
	bad := append(append([]byte{}, rawdb.SnapshotAccountPrefix...), append(a1.Bytes(), 0x00)...)

	require.NoError(t, db.Put(key1, []byte("v1")))
	require.NoError(t, db.Put(key2, []byte("v2")))
	require.NoError(t, db.Put(bad, []byte("nope")))

	it := NewAccountSnapshotsIterator(db)
	defer it.Release()
	count := 0
	for it.Next() {
		count++
	}
	require.Equal(t, 2, count)
}
