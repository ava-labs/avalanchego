// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/stretchr/testify/require"
)

func TestTimeMarkers(t *testing.T) {
	cases := []struct {
		name   string
		write  func(ethdb.KeyValueStore) error
		read   func(ethdb.KeyValueStore) (time.Time, error)
		delete func(ethdb.KeyValueStore) error
	}{
		{
			name:   "offline_pruning",
			read:   ReadOfflinePruning,
			write:  WriteOfflinePruning,
			delete: DeleteOfflinePruning,
		},
		{
			name:   "populate_missing_tries",
			read:   ReadPopulateMissingTries,
			write:  WritePopulateMissingTries,
			delete: DeletePopulateMissingTries,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()

			// Not present initially.
			_, err := tc.read(db)
			require.ErrorIs(t, err, errMarkerNotFound)

			// Write marker and read back a reasonable recent time.
			require.NoError(t, tc.write(db))
			ts, err := tc.read(db)
			require.NoError(t, err)
			require.WithinDuration(t, time.Now(), ts, 2*time.Second)

			// Delete marker.
			require.NoError(t, tc.delete(db))
			_, err = tc.read(db)
			require.ErrorIs(t, err, errMarkerNotFound)
		})
	}
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
	require.ErrorIs(t, err, errAcceptorTipInvalid)
}

func TestWriteAcceptorTip(t *testing.T) {
	cases := []struct {
		name   string
		writes []common.Hash
		want   common.Hash
	}{
		{
			name:   "none",
			writes: nil,
			want:   common.Hash{},
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

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			for _, h := range tc.writes {
				require.NoError(t, WriteAcceptorTip(db, h))
			}
			tip, err := ReadAcceptorTip(db)
			require.NoError(t, err)
			require.Equal(t, tc.want, tip)
		})
	}
}

func TestTimeMarkers_BadEncoding(t *testing.T) {
	// Validate that decode errors are surfaced and are not the sentinel not-found error.
	cases := []struct {
		name string
		key  []byte
		read func(ethdb.KeyValueStore) (time.Time, error)
	}{
		{
			name: "offline_pruning",
			key:  offlinePruningKey,
			read: ReadOfflinePruning,
		},
		{
			name: "populate_missing_tries",
			key:  populateMissingTriesKey,
			read: ReadPopulateMissingTries,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			// Write invalid RLP bytes (0xB8 indicates a long string length with missing payload).
			require.NoError(t, db.Put(tc.key, []byte{0xB8}))
			_, err := tc.read(db)
			require.ErrorIs(t, err, errMarkerInvalid)
		})
	}
}
