// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/imp/blackhole"
	"github.com/ava-labs/avalanchego/vms/evm/saedb"
)

func TestParseConfig_FeeRecipient(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    common.Address
		wantErr bool
	}{
		{
			name: "unset_burns",
			json: `{}`,
			want: blackhole.Address,
		},
		{
			name: "address",
			json: `{"fee-recipient":"0x0123456789abcdef0123456789abcdef01234567"}`,
			want: common.HexToAddress("0x0123456789abcdef0123456789abcdef01234567"),
		},
		{
			name:    "missing_prefix",
			json:    `{"fee-recipient":"0123456789abcdef0123456789abcdef01234567"}`,
			wantErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, []byte(test.json))
			if test.wantErr {
				var typeErr *json.UnmarshalTypeError
				require.ErrorAs(t, err, &typeErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, c.FeeRecipient)
		})
	}
}

// TestParseConfig_DefaultsAndOverrides asserts that ParseConfig
// pre-populates from DefaultConfig (legacy-compatible defaults) and
// that operator overrides via flat legacy-shaped JSON keys take
// effect.
func TestParseConfig_DefaultsAndOverrides(t *testing.T) {
	defaults := defaultConfig("")

	t.Run("nil_bytes_yields_defaults", func(t *testing.T) {
		c, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, nil)
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})

	t.Run("empty_object_yields_defaults", func(t *testing.T) {
		c, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, []byte(`{}`))
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})

	t.Run("partial_override_keeps_other_defaults", func(t *testing.T) {
		c, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, []byte(`{"database-type":"leveldb","commit-interval":8192}`))
		require.NoError(t, err)
		want := defaultConfig("")
		want.DatabaseType = "leveldb"
		want.CommitInterval = 8192
		require.Equal(t, want, c)
	})

	t.Run("unknown_field_ignored", func(t *testing.T) {
		// Legacy keys with no SAE counterpart MUST NOT prevent an existing
		// node config from starting the VM.
		c, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, []byte(`{"airdrop":"genesis.json"}`))
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})

	t.Run("duration_string", func(t *testing.T) {
		c, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, []byte(`{"api-max-duration":"30m"}`))
		require.NoError(t, err)
		require.Equal(t, 30*time.Minute, c.APIMaxDuration.Duration)
	})

	t.Run("duration_numeric_rejected", func(t *testing.T) {
		_, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, []byte(`{"api-max-duration":900000000000}`))
		var typeErr *json.UnmarshalTypeError
		require.ErrorAs(t, err, &typeErr)
	})

	t.Run("marshal_roundtrip", func(t *testing.T) {
		out, err := json.Marshal(defaults)
		require.NoError(t, err)
		c, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, out)
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})
}

// TestConfig_SAEConfig spot-checks the [sae.Config] produced from operator
// fields so the Initialize-time wiring keeps matching what operators wrote.
func TestConfig_SAEConfig(t *testing.T) {
	c := defaultConfig("")
	c.LocalTxsEnabled = true
	c.Pruning = false
	c.CommitInterval = 8192

	got := c.saeConfig(nil)
	require.False(t, got.MempoolConfig.NoLocals, "LocalTxsEnabled=true => NoLocals=false")
	require.Empty(t, got.MempoolConfig.Journal)
	require.True(t, got.DBConfig.Archival, "Pruning=false => Archival=true")
	require.Equal(t, uint64(8192), got.DBConfig.CommitInterval)

	// Defaults round-trip to the legacy-equivalent SAE state: pruning on
	// (Archival=false), commit interval at the saedb default.
	d := defaultConfig("").saeConfig(nil)
	require.False(t, d.DBConfig.Archival)
	require.Equal(t, uint64(saedb.DefaultCommitInterval), d.DBConfig.CommitInterval)
}

// TestParseConfig_ValidatesSAEConfig pins parse-time validation of the
// derived SAE configuration: values the SAE core would only reject at
// runtime (or worse, divide by) MUST fail at chain-config parse time.
func TestParseConfig_ValidatesSAEConfig(t *testing.T) {
	_, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, []byte(`{"commit-interval":0}`))
	require.ErrorIs(t, err, saedb.Config{}.Verify(), "parseConfig()")
}

func TestParseConfig_RPC(t *testing.T) {
	tests := []struct {
		name            string
		config          string
		wantDuration    time.Duration
		wantBatchLimit  uint64
		wantResolveLast bool
	}{
		{
			name:            "string_duration",
			config:          `{"api-max-duration":"5s","batch-request-limit":25,"api-resolve-pending-to-last-executed":true}`,
			wantDuration:    5 * time.Second,
			wantBatchLimit:  25,
			wantResolveLast: true,
		},
		{
			name:            "disable_resolve_pending",
			config:          `{"api-max-duration":"10s","batch-request-limit":50,"api-resolve-pending-to-last-executed":false}`,
			wantDuration:    10 * time.Second,
			wantBatchLimit:  50,
			wantResolveLast: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := parseConfig(&snow.Context{Log: logging.NoLog{}}, []byte(test.config))
			require.NoError(t, err, "parseConfig(..., %q)", test.config)

			rpcConfig := config.saeConfig(nil).RPCConfig
			require.Equal(t, test.wantDuration, rpcConfig.CallTimeout, "parseConfig(..., %q)", test.config)
			require.Equal(t, test.wantBatchLimit, rpcConfig.BatchRequestLimit, "parseConfig(..., %q)", test.config)
			require.Equal(t, test.wantResolveLast, rpcConfig.ResolvePendingToLastExecuted, "parseConfig(..., %q)", test.config)
		})
	}
}

func TestDefaultConfig_Resources(t *testing.T) {
	config := defaultConfig("").saeConfig(nil)
	require.Equal(t, uint64(1000), config.RPCConfig.BatchRequestLimit, `defaultConfig("").saeConfig(nil)`)
	require.True(t, config.RPCConfig.ResolvePendingToLastExecuted, `defaultConfig("").saeConfig(nil)`)
	require.Equal(t, uint64(saedb.DefaultTrieCacheSizeMiB), config.DBConfig.TrieCacheMiB, `defaultConfig("").saeConfig(nil)`)
	require.Equal(t, uint64(saedb.DefaultSnapshotCacheSizeMiB), config.DBConfig.SnapshotCacheMiB, `defaultConfig("").saeConfig(nil)`)
}
