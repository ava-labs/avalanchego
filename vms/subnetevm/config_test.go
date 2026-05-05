// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
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
			c, err := ParseConfig(test.bytes)
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
	defaults := DefaultConfig()

	t.Run("nil_bytes_yields_defaults", func(t *testing.T) {
		c, err := ParseConfig(nil)
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})

	t.Run("empty_object_yields_defaults", func(t *testing.T) {
		c, err := ParseConfig([]byte(`{}`))
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})

	t.Run("partial_override_keeps_other_defaults", func(t *testing.T) {
		c, err := ParseConfig([]byte(`{"tx-pool-price-limit":42,"rpc-gas-cap":1234}`))
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
		_, err := ParseConfig([]byte(`{"state-sync-enabled":true}`))
		// JSON decoder does not expose a sentinel error for unknown fields,
		// so we check for the presence of the field name and "unknown field" in the error message.

		require.NotNil(t, err)
		require.Contains(t, err.Error(), "state-sync-enabled")
		require.Contains(t, err.Error(), "unknown field")
	})

	t.Run("duration_accepts_string_and_numeric", func(t *testing.T) {
		t.Run("string", func(t *testing.T) {
			c, err := ParseConfig([]byte(`{"tx-pool-lifetime":"30m"}`))
			require.NoError(t, err)
			require.Equal(t, 30*time.Minute, c.TxPoolLifetime.Duration)
		})
		t.Run("numeric_nanoseconds", func(t *testing.T) {
			c, err := ParseConfig([]byte(`{"tx-pool-lifetime":900000000000}`))
			require.NoError(t, err)
			require.Equal(t, 15*time.Minute, c.TxPoolLifetime.Duration)
		})
	})

	t.Run("marshal_roundtrip", func(t *testing.T) {
		out, err := json.Marshal(defaults)
		require.NoError(t, err)
		c, err := ParseConfig(out)
		require.NoError(t, err)
		require.Equal(t, defaults, c)
	})

	t.Run("log_level_and_format", func(t *testing.T) {
		c, err := ParseConfig([]byte(`{"log-level":"debug"}`))
		require.NoError(t, err)
		require.Equal(t, "debug", c.LogLevel)
	})
}

// TestConfig_Converters spot-checks the SAE-config-shape outputs of
// the converter methods so the Initialize-time wiring keeps matching
// what operators wrote.
func TestConfig_Converters(t *testing.T) {
	c := DefaultConfig()
	c.RPCGasCap = 1
	c.RPCTxFeeCap = 2
	c.LocalTxsEnabled = true
	c.TxPoolPriceLimit = 3
	c.TxPoolLifetime = Duration{42 * time.Second}
	c.PruningEnabled = false
	c.CommitInterval = 8192

	mp := c.toMempoolConfig()
	require.False(t, mp.NoLocals, "LocalTxsEnabled=true => NoLocals=false")
	require.Equal(t, uint64(3), mp.PriceLimit)
	require.Equal(t, 42*time.Second, mp.Lifetime)

	rp := c.toRPCConfig()
	require.Equal(t, uint64(1), rp.GasCap)
	require.InDelta(t, 2.0, rp.TxFeeCap, 0)

	db := c.toDBConfig()
	require.NotNil(t, db.TrieDBConfig, "saedb.Config.TrieDBConfig must default to triedb.HashDefaults")
	require.True(t, db.Archival, "PruningEnabled=false => Archival=true")
	require.Equal(t, uint64(8192), db.TrieCommitInterval)

	// Defaults round-trip through the converter to the legacy-equivalent
	// SAE state: pruning on (Archival=false), commit interval 4096.
	d := DefaultConfig().toDBConfig()
	require.False(t, d.Archival)
	require.Equal(t, uint64(4096), d.TrieCommitInterval)
}

func TestConfig_WarpMessages(t *testing.T) {
	payload, err := payload.NewAddressedCall(
		utils.RandomBytes(20),
		[]byte("test"),
	)
	require.NoError(t, err)

	msg, err := warp.NewUnsignedMessage(12345, ids.GenerateTestID(), payload.Bytes())
	require.NoError(t, err)

	tests := []struct {
		name    string
		bytes   [][]byte
		want    []*warp.UnsignedMessage
		wantErr error
	}{
		{
			name: "empty",
			want: []*warp.UnsignedMessage{},
		},
		{
			name:  "single_message",
			bytes: [][]byte{msg.Bytes()},
			want:  []*warp.UnsignedMessage{msg},
		},
		{
			name:  "multiple_messages",
			bytes: [][]byte{msg.Bytes(), msg.Bytes()},
			want:  []*warp.UnsignedMessage{msg, msg},
		},
		{
			name:    "invalid_message",
			bytes:   [][]byte{{0xff}},
			wantErr: errParsingWarpMessage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var c Config
			for _, msgBytes := range test.bytes {
				c.WarpOffChainMessages = append(c.WarpOffChainMessages, msgBytes)
			}

			got, err := c.WarpMessages()
			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, test.want, got)
		})
	}
}
