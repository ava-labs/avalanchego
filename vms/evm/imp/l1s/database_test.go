// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/types"
)

// TestConfig_UseStandaloneDatabase asserts that an explicit setting MUST be
// honored, and that the default MUST keep an existing chain in the node's
// database.
func TestConfig_UseStandaloneDatabase(t *testing.T) {
	withChain := memdb.New()
	rawdb.WriteCanonicalHash(types.NewChainEthDB(withChain), common.Hash{1}, genesisNumber)

	tests := []struct {
		name      string
		setting   *bool
		networkID uint32
		avaDB     *memdb.Database
		want      bool
	}{
		{
			name:      "explicit_true_with_chain",
			setting:   new(true),
			networkID: constants.UnitTestID,
			avaDB:     withChain,
			want:      true,
		},
		{
			name:      "explicit_false_without_chain",
			setting:   new(false),
			networkID: constants.MainnetID,
			avaDB:     memdb.New(),
			want:      false,
		},
		{
			name:      "default_without_chain",
			networkID: constants.MainnetID,
			avaDB:     memdb.New(),
			want:      true,
		},
		{
			name:      "default_with_chain",
			networkID: constants.MainnetID,
			avaDB:     withChain,
			want:      false,
		},
		{
			name:      "default_on_unit_test_network",
			networkID: constants.UnitTestID,
			avaDB:     memdb.New(),
			want:      false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := defaultConfig()
			c.UseStandaloneDatabase = test.setting
			require.Equal(t, test.want, c.useStandaloneDatabase(test.networkID, test.avaDB))
		})
	}
}

func TestConfig_DatabaseConfig(t *testing.T) {
	const levelConfig = `{"blockCacheCapacity":1}`
	levelFile := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(levelFile, []byte(levelConfig), 0o600))

	pebbleNoSync := pebbledb.DefaultConfig
	pebbleNoSync.Sync = false
	pebbleOverridden := pebbleNoSync
	pebbleOverridden.MaxOpenFiles = 1

	tests := []struct {
		name    string
		c       config
		want    []byte
		wantErr error
	}{
		{
			name: "pebble_defaults_to_no_sync",
			c:    config{DatabaseType: pebbledb.Name},
			want: mustMarshalJSON(t, pebbleNoSync),
		},
		{
			name: "pebble_content_overrides_defaults",
			c: config{
				DatabaseType:          pebbledb.Name,
				DatabaseConfigContent: base64.StdEncoding.EncodeToString([]byte(`{"maxOpenFiles":1}`)),
			},
			want: mustMarshalJSON(t, pebbleOverridden),
		},
		{
			name: "content_precedes_file",
			c: config{
				DatabaseType:          leveldb.Name,
				DatabaseConfigContent: base64.StdEncoding.EncodeToString([]byte(levelConfig)),
				DatabaseConfigFile:    filepath.Join(t.TempDir(), "missing.json"),
			},
			want: []byte(levelConfig),
		},
		{
			name: "file",
			c: config{
				DatabaseType:       leveldb.Name,
				DatabaseConfigFile: levelFile,
			},
			want: []byte(levelConfig),
		},
		{
			name: "invalid_base64",
			c: config{
				DatabaseType:          leveldb.Name,
				DatabaseConfigContent: "!",
			},
			wantErr: base64.CorruptInputError(0),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.c.databaseConfig()
			require.ErrorIs(t, err, test.wantErr)
			if test.wantErr == nil {
				require.JSONEq(t, string(test.want), string(got))
			}
		})
	}
}

// TestStandaloneDatabase asserts that a standalone database MUST hold all
// chain data, so the chain resumes from it after a restart while the node's
// database stays empty.
func TestStandaloneDatabase(t *testing.T) {
	sut := newL1SUT(
		t,
		withFork(upgradetest.Igloo),
		withNow(postIglooStartTime(t)),
		withStandaloneDatabase(t.TempDir()),
	)

	sut.advanceTime(t, blockBuildAdvance)
	sut.sendTransferTx(t, 0, 0, common.Big1)
	want := sut.buildAcceptExecuteBlock(t)

	sut.restart(t)

	got, err := sut.vm.LastAccepted(sut.ctx)
	require.NoError(t, err)
	require.Equal(t, want.ID(), got, "LastAccepted() after restart")

	it := sut.baseDB.NewIterator()
	defer it.Release()
	require.False(t, it.Next(), "node database has key %x", it.Key())
	require.NoError(t, it.Error())
}
