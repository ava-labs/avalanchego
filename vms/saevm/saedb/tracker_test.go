// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
)

func TestNewTracker(t *testing.T) {
	defaults := Config{CommitInterval: 1}

	tests := []struct {
		name    string
		with    func(*Config)
		wantErr error
	}{
		{
			name: "defaults",
		},
		{
			name:    "zero_commit_interval",
			with:    func(c *Config) { c.CommitInterval = 0 },
			wantErr: errZeroCommitInterval,
		},
		{
			name: "with_snapshot",
			with: func(c *Config) {
				c.SnapshotCacheMiB = 1
			},
		},
		{
			name:    "trie_cache_overflows_bytes",
			with:    func(c *Config) { c.TrieCacheMiB = math.MaxInt },
			wantErr: errCacheTooLarge,
		},
		{
			name:    "snapshot_cache_overflows_bytes",
			with:    func(c *Config) { c.SnapshotCacheMiB = math.MaxInt },
			wantErr: errCacheTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := loggingtest.New(t, logging.Debug)
			cfg := defaults
			if tt.with != nil {
				tt.with(&cfg)
			}
			db := rawdb.NewMemoryDatabase()

			tr, err := NewTracker(db, cfg, types.EmptyRootHash, log)
			require.ErrorIs(t, err, tt.wantErr, "NewTracker()")
			if err != nil {
				return
			}
			require.NoErrorf(t, tr.Close(types.EmptyRootHash), "%T.Close()", tr)

			// If the snapshot is enabled, we would expect to find the root on disk.
			var wantRoot common.Hash
			if cfg.SnapshotCacheMiB > 0 {
				wantRoot = types.EmptyRootHash
			}
			gotRoot := rawdb.ReadSnapshotRoot(db)
			require.Equal(t, wantRoot, gotRoot, "rawdb.ReadSnapshotRoot()")
		})
	}
}

func TestProtectTrieIndex(t *testing.T) {
	tests := []struct {
		name    string
		runs    []Config // node runs against the same DB; only the last may error
		wantErr error
	}{
		{
			name: "archival",
			runs: []Config{{Archival: true}},
		},
		{
			name: "pruning_fresh_db",
			runs: []Config{{}},
		},
		{
			name:    "pruning_after_archival",
			runs:    []Config{{Archival: true}, {}},
			wantErr: errRefuseToCorruptArchiver,
		},
		{
			name: "pruning_after_archival_allowed",
			runs: []Config{{Archival: true}, {AllowMissingTries: true}},
		},
		{
			name:    "archival_history_outlives_allowed_pruning_run",
			runs:    []Config{{Archival: true}, {AllowMissingTries: true}, {}},
			wantErr: errRefuseToCorruptArchiver,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			for _, c := range tt.runs[:len(tt.runs)-1] {
				require.NoError(t, protectTrieIndex(db, c), "protectTrieIndex(%+v)", c)
			}
			last := tt.runs[len(tt.runs)-1]
			require.ErrorIs(t, protectTrieIndex(db, last), tt.wantErr, "protectTrieIndex(%+v)", last)
		})
	}
}
