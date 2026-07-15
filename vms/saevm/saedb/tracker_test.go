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

// TestProtectTrieIndex simulates every pair of consecutive node runs against
// the same database. The first run, on a fresh database, always succeeds;
// only the second may error.
func TestProtectTrieIndex(t *testing.T) {
	configs := map[string]Config{
		"archival":        {Archival: true},
		"pruning":         {},
		"allowed_pruning": {AllowMissingTries: true},
	}
	wantErrs := map[string]error{
		"pruning_after_archival": errRefuseToCorruptArchiver,
	}

	for name1, config1 := range configs {
		for name2, config2 := range configs {
			name := name2 + "_after_" + name1
			t.Run(name, func(t *testing.T) {
				db := rawdb.NewMemoryDatabase()
				require.NoError(t, protectTrieIndex(db, config1), "protectTrieIndex(%+v) on fresh DB", config1)
				require.ErrorIs(t, protectTrieIndex(db, config2), wantErrs[name], "protectTrieIndex(%+v) after first run", config2)
			})
		}
	}
}
