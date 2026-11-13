// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/leveldb"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/types"
)

// TestConfig_UseStandaloneDatabase asserts that a chain already in the node's
// database MUST stay there, and that otherwise the setting MUST be honored.
func TestConfig_UseStandaloneDatabase(t *testing.T) {
	withChain := memdb.New()
	rawdb.WriteCanonicalHash(types.NewChainEthDB(withChain), common.Hash{1}, genesisNumber)

	tests := []struct {
		name         string
		setting      bool
		avaDB        *memdb.Database
		want         bool
		wantWarnings []*loggingtest.Record
	}{
		{
			name:    "true_without_chain",
			setting: true,
			avaDB:   memdb.New(),
			want:    true,
		},
		{
			name:    "false_without_chain",
			setting: false,
			avaDB:   memdb.New(),
			want:    false,
		},
		{
			name:    "true_with_chain",
			setting: true,
			avaDB:   withChain,
			want:    false,
			wantWarnings: []*loggingtest.Record{{
				Level: logging.Warn,
				Msg:   `ignoring "use-standalone-database" because the node's database already holds the chain`,
			}},
		},
		{
			name:    "false_with_chain",
			setting: false,
			avaDB:   withChain,
			want:    false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			log := loggingtest.NewRecorder(logging.Warn)
			c := defaultConfig("")
			c.UseStandaloneDatabase = test.setting
			require.Equal(t, test.want, c.useStandaloneDatabase(log, test.avaDB), "useStandaloneDatabase(...)")
			require.Equal(t, test.wantWarnings, log.Records, "useStandaloneDatabase(...) logs")
		})
	}
}

func TestConfig_DatabaseConfig(t *testing.T) {
	const levelConfig = `{"blockCacheCapacity":1}`

	pebbleNoSync := pebbledb.DefaultConfig
	pebbleNoSync.Sync = false
	pebbleOverridden := pebbleNoSync
	pebbleOverridden.MaxOpenFiles = 1

	tests := []struct {
		name string
		c    config
		want []byte
	}{
		{
			name: "pebble_defaults_to_no_sync",
			c:    config{DatabaseType: pebbledb.Name},
			want: mustMarshalJSON(t, pebbleNoSync),
		},
		{
			name: "pebble_config_overrides_defaults",
			c: config{
				DatabaseType:   pebbledb.Name,
				DatabaseConfig: json.RawMessage(`{"maxOpenFiles":1}`),
			},
			want: mustMarshalJSON(t, pebbleOverridden),
		},
		{
			name: "leveldb_config_unchanged",
			c: config{
				DatabaseType:   leveldb.Name,
				DatabaseConfig: json.RawMessage(levelConfig),
			},
			want: []byte(levelConfig),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.c.databaseConfig()
			require.NoError(t, err)
			require.JSONEq(t, string(test.want), string(got))
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
