// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
)

func TestParseConfig_FeeRecipient(t *testing.T) {
	tests := []struct {
		name    string
		bytes   []byte
		want    string
		wantErr error
	}{
		{
			name:  "empty_bytes",
			bytes: nil,
			want:  "",
		},
		{
			name:  "absent_field",
			bytes: []byte(`{}`),
			want:  "",
		},
		{
			name:  "explicit_empty_string",
			bytes: []byte(`{"feeRecipient":""}`),
			want:  "",
		},
		{
			name:  "valid_hex_address_with_prefix",
			bytes: []byte(`{"feeRecipient":"0x0123456789abcdef0123456789abcdef01234567"}`),
			want:  "0x0123456789abcdef0123456789abcdef01234567",
		},
		{
			name:  "valid_hex_address_without_prefix",
			bytes: []byte(`{"feeRecipient":"0123456789abcdef0123456789abcdef01234567"}`),
			want:  "0123456789abcdef0123456789abcdef01234567",
		},
		{
			name:    "invalid_hex_address_too_short",
			bytes:   []byte(`{"feeRecipient":"0xdead"}`),
			wantErr: errInvalidFeeRecipient,
		},
		{
			name:    "invalid_hex_address_garbage",
			bytes:   []byte(`{"feeRecipient":"not-an-address"}`),
			wantErr: errInvalidFeeRecipient,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, err := parseConfig(test.bytes)
			require.ErrorIs(t, err, test.wantErr)
			if test.wantErr == nil {
				require.Equal(t, test.want, c.FeeRecipient)
			}
		})
	}
}

// TestParseConfig_DefaultsAndOverrides asserts that ParseConfig
// pre-populates from DefaultConfig (legacy-compatible defaults) and
// that operator overrides via flat legacy-shaped JSON keys take
// effect.
func TestParseConfig_DefaultsAndOverrides(t *testing.T) {
	defaults := defaultConfig()

	t.Run("nil_bytes_yields_defaults", func(t *testing.T) {
		c, err := parseConfig(nil)
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})

	t.Run("empty_object_yields_defaults", func(t *testing.T) {
		c, err := parseConfig([]byte(`{}`))
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})

	t.Run("partial_override_keeps_other_defaults", func(t *testing.T) {
		c, err := parseConfig([]byte(`{"tx-pool-price-limit":42,"rpc-gas-cap":1234}`))
		require.NoError(t, err)
		require.Equal(t, uint64(42), c.TxPoolPriceLimit)
		require.Equal(t, uint64(1234), c.RPCGasCap)
		require.Equal(t, defaults.TxPoolPriceBump, c.TxPoolPriceBump)
		require.Equal(t, defaults.RPCTxFeeCap, c.RPCTxFeeCap)
		require.Equal(t, defaults.TxPoolLifetime, c.TxPoolLifetime)
	})

	t.Run("unknown_field_rejected", func(t *testing.T) {
		// Any legacy key that has no SAE counterpart (here
		// `state-sync-enabled`, but stands in for ~60 others) must
		// surface as an explicit decoder error rather than silently
		// no-op.
		_, err := parseConfig([]byte(`{"state-sync-enabled":true}`))
		// JSON decoder does not expose a sentinel error for unknown fields,
		// so we check for the presence of the field name and "unknown field" in the error message.
		require.Contains(t, err.Error(), "state-sync-enabled")
		require.Contains(t, err.Error(), "unknown field")
	})

	t.Run("duration_accepts_string_and_numeric", func(t *testing.T) {
		t.Run("string", func(t *testing.T) {
			c, err := parseConfig([]byte(`{"tx-pool-lifetime":"30m"}`))
			require.NoError(t, err)
			require.Equal(t, 30*time.Minute, c.TxPoolLifetime.Duration)
		})
		t.Run("numeric_nanoseconds", func(t *testing.T) {
			c, err := parseConfig([]byte(`{"tx-pool-lifetime":900000000000}`))
			require.NoError(t, err)
			require.Equal(t, 15*time.Minute, c.TxPoolLifetime.Duration)
		})
	})

	t.Run("marshal_roundtrip", func(t *testing.T) {
		out, err := json.Marshal(defaults)
		require.NoError(t, err)
		c, err := parseConfig(out)
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})

	t.Run("log_level_and_format", func(t *testing.T) {
		c, err := parseConfig([]byte(`{"log-level":"debug"}`))
		require.NoError(t, err)
		require.Equal(t, "debug", c.LogLevel)
	})
}

// TestConfig_SAEConfig spot-checks the [sae.Config] produced from operator
// fields so the Initialize-time wiring keeps matching what operators wrote.
func TestConfig_SAEConfig(t *testing.T) {
	c := defaultConfig()
	c.RPCGasCap = 1
	c.RPCTxFeeCap = 2
	c.LocalTxsEnabled = true
	c.TxPoolPriceLimit = 3
	c.TxPoolLifetime = duration{42 * time.Second}
	c.PruningEnabled = false
	c.CommitInterval = 8192

	got := c.saeConfig(nil)
	require.False(t, got.MempoolConfig.NoLocals, "LocalTxsEnabled=true => NoLocals=false")
	require.Equal(t, uint64(3), got.MempoolConfig.PriceLimit)
	require.Equal(t, 42*time.Second, got.MempoolConfig.Lifetime)
	require.Equal(t, uint64(1), got.RPCConfig.GasCap)
	require.InDelta(t, 2.0, got.RPCConfig.TxFeeCap, 0)
	require.True(t, got.DBConfig.Archival, "PruningEnabled=false => Archival=true")
	require.Equal(t, uint64(8192), got.DBConfig.CommitInterval)

	// Defaults round-trip to the legacy-equivalent SAE state: pruning on
	// (Archival=false), commit interval at the saedb default.
	d := defaultConfig().saeConfig(nil)
	require.False(t, d.DBConfig.Archival)
	require.Equal(t, uint64(saedb.DefaultCommitInterval), d.DBConfig.CommitInterval)
}

// TestParseConfig_ValidatesSAEConfig pins parse-time validation of the
// derived SAE configuration: values the SAE core would only reject at
// runtime (or worse, divide by) MUST fail at chain-config parse time.
func TestParseConfig_ValidatesSAEConfig(t *testing.T) {
	_, err := parseConfig([]byte(`{"commit-interval":0}`))
	require.ErrorIs(t, err, saedb.ErrZeroCommitInterval, "parseConfig()")
}

func TestParseConfig_RejectsTrailingData(t *testing.T) {
	_, err := parseConfig([]byte(`{} {}`))
	require.ErrorIs(t, err, errTrailingConfigData, "parseConfig()")
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
			name:            "numeric_duration",
			config:          `{"api-max-duration":5000000000,"batch-request-limit":50,"api-resolve-pending-to-last-executed":false}`,
			wantDuration:    5 * time.Second,
			wantBatchLimit:  50,
			wantResolveLast: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := parseConfig([]byte(test.config))
			require.NoError(t, err, "parseConfig(%q)", test.config)

			rpcConfig := config.saeConfig(nil).RPCConfig
			require.Equal(t, test.wantDuration, rpcConfig.EVMTimeout, "parseConfig(%q)", test.config)
			require.Equal(t, test.wantBatchLimit, rpcConfig.BatchRequestLimit, "parseConfig(%q)", test.config)
			require.Equal(t, test.wantResolveLast, rpcConfig.ResolvePendingToLastExecuted, "parseConfig(%q)", test.config)
		})
	}
}

func TestDefaultConfig_Resources(t *testing.T) {
	config := defaultConfig().saeConfig(nil)
	require.Equal(t, uint64(1000), config.RPCConfig.BatchRequestLimit, "defaultConfig().saeConfig(nil)")
	require.True(t, config.RPCConfig.ResolvePendingToLastExecuted, "defaultConfig().saeConfig(nil)")
	require.Equal(t, uint64(saedb.DefaultTrieCacheSizeMiB), config.DBConfig.TrieCacheMiB, "defaultConfig().saeConfig(nil)")
	require.Equal(t, uint64(saedb.DefaultSnapshotCacheSizeMiB), config.DBConfig.SnapshotCacheMiB, "defaultConfig().saeConfig(nil)")
}
