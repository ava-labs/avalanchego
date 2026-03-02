// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
)

func TestOfflinePruning(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Not present initially.
	_, err := ReadOfflinePruning(db)
	require.ErrorIs(t, err, database.ErrNotFound)

	// Write marker and read back fixed time.
	fixed := time.Unix(1_700_000_000, 0)
	require.NoError(t, WriteOfflinePruning(db, fixed))
	ts, err := ReadOfflinePruning(db)
	require.NoError(t, err)
	require.Equal(t, fixed.Unix(), ts.Unix())

	// Delete marker.
	require.NoError(t, DeleteOfflinePruning(db))
	_, err = ReadOfflinePruning(db)
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestPopulateMissingTries(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Not present initially.
	_, err := ReadPopulateMissingTries(db)
	require.ErrorIs(t, err, database.ErrNotFound)

	// Write marker and read back fixed time.
	fixed := time.Unix(1_700_000_000, 0)
	require.NoError(t, WritePopulateMissingTries(db, fixed))
	ts, err := ReadPopulateMissingTries(db)
	require.NoError(t, err)
	require.Equal(t, fixed.Unix(), ts.Unix())

	// Delete marker.
	require.NoError(t, DeletePopulateMissingTries(db))
	_, err = ReadPopulateMissingTries(db)
	require.ErrorIs(t, err, database.ErrNotFound)
}

func TestOfflinePruning_BadEncoding(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	// Write invalid RLP bytes (0xB8 indicates a long string length with missing payload).
	require.NoError(t, db.Put(offlinePruningKey, []byte{0xB8}))
	_, err := ReadOfflinePruning(db)
	require.ErrorIs(t, err, errInvalidData)
}

func TestPopulateMissingTries_BadEncoding(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	// Write invalid RLP bytes (0xB8 indicates a long string length with missing payload).
	require.NoError(t, db.Put(populateMissingTriesKey, []byte{0xB8}))
	_, err := ReadPopulateMissingTries(db)
	require.ErrorIs(t, err, errInvalidData)
}

func TestPruningDisabledFlag(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	ok, err := HasPruningDisabled(db)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, WritePruningDisabled(db))

	ok, err = HasPruningDisabled(db)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestReadAcceptorTip_InvalidLength(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	// Write an invalid value under acceptor tip key (wrong length).
	require.NoError(t, db.Put(acceptorTipKey, []byte("short")))
	_, err := ReadAcceptorTip(db)
	require.ErrorIs(t, err, errInvalidData)
}

func TestWriteAcceptorTip(t *testing.T) {
	tests := []struct {
		name    string
		writes  []common.Hash
		want    common.Hash
		wantErr error
	}{
		{
			name:    "none",
			writes:  nil,
			want:    common.Hash{},
			wantErr: database.ErrNotFound,
		},
		{
			name:   "single_write",
			writes: []common.Hash{common.HexToHash("0xabc1")},
			want:   common.HexToHash("0xabc1"),
		},
		{
			name:   "overwrite",
			writes: []common.Hash{common.HexToHash("0xabc1"), common.HexToHash("0xabc2")},
			want:   common.HexToHash("0xabc2"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			for _, h := range tc.writes {
				require.NoError(t, WriteAcceptorTip(db, h))
			}
			tip, err := ReadAcceptorTip(db)
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, tip)
		})
	}
}

func TestSnapshotBlockHashReadWriteDelete(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Initially empty.
	got, err := ReadSnapshotBlockHash(db)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Equal(t, common.Hash{}, got)

	// Write and read back.
	want := common.HexToHash("0xdeadbeef")
	require.NoError(t, WriteSnapshotBlockHash(db, want))
	got, err = ReadSnapshotBlockHash(db)
	require.NoError(t, err)
	require.Equal(t, want, got)

	// Delete and verify empty.
	require.NoError(t, DeleteSnapshotBlockHash(db))
	got, err = ReadSnapshotBlockHash(db)
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Equal(t, common.Hash{}, got)
}

func TestNewAccountSnapshotsIterator(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	// Keys that match and don't match the iterator length filter.
	a1 := common.HexToHash("0x01")
	a2 := common.HexToHash("0x02")
	key1 := slices.Concat(rawdb.SnapshotAccountPrefix, a1.Bytes())
	key2 := slices.Concat(rawdb.SnapshotAccountPrefix, a2.Bytes())
	// Non-matching: extra byte appended.
	bad := slices.Concat(key1, []byte{0x00})

	require.NoError(t, db.Put(key1, []byte("v1")))
	require.NoError(t, db.Put(key2, []byte("v2")))
	require.NoError(t, db.Put(bad, []byte("nope")))

	it := NewAccountSnapshotsIterator(db)
	defer it.Release()
	count := 0
	for it.Next() {
		count++
	}
	require.NoError(t, it.Error())
	require.Equal(t, 2, count)
}

func TestSnapshotBlockHash_InvalidLength(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	// Write wrong length value and assert invalid encoding.
	require.NoError(t, db.Put(snapshotBlockHashKey, []byte("short")))
	_, err := ReadSnapshotBlockHash(db)
	require.ErrorIs(t, err, errInvalidData)
}

func TestChainConfigCases(t *testing.T) {
	type upgrade struct {
		X int `json:"x"`
	}

	tests := []struct {
		name         string
		cfg          *params.ChainConfig
		inputUpgrade *upgrade // nil => no overwrite
		wantErr      error
	}{
		{
			name:         "valid_upgrade",
			cfg:          &params.ChainConfig{ChainID: big.NewInt(1)},
			inputUpgrade: &upgrade{X: 7},
		},
		{
			name:    "nil_config",
			wantErr: database.ErrNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			h := common.HexToHash("0x100")

			require.NoError(t, WriteChainConfig(db, h, tc.cfg, upgrade{X: 0}))
			if tc.inputUpgrade != nil && tc.cfg != nil {
				require.NoError(t, WriteChainConfig(db, h, tc.cfg, *tc.inputUpgrade))
			}

			var out upgrade
			_, err := ReadChainConfig(db, h, &out)
			require.ErrorIs(t, err, tc.wantErr)
			if tc.wantErr == nil {
				require.Equal(t, *tc.inputUpgrade, out)
			}
		})
	}
}

func TestReadChainConfig_InvalidUpgradeJSONReturnsNil(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	hash := common.HexToHash("0xbeef")
	// Write a valid base chain config.
	rawdb.WriteChainConfig(db, hash, &params.ChainConfig{})
	// Write invalid upgrade JSON.
	require.NoError(t, db.Put(upgradeConfigKey(hash), []byte("{")))

	var out struct{}
	got, err := ReadChainConfig(db, hash, &out)
	require.ErrorIs(t, err, errInvalidData)
	require.Nil(t, got)
}
