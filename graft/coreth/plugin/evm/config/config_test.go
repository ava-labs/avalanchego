// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

// newTrue returns a pointer to a bool that is true
func newTrue() *bool {
	b := true
	return &b
}

func TestUnmarshalConfig(t *testing.T) {
	tests := []struct {
		name        string
		givenJSON   []byte
		expected    Config
		expectedErr bool
	}{
		{
			"string durations parsed",
			[]byte(`{"api-max-duration": "1m", "continuous-profiler-frequency": "2m"}`),
			Config{APIMaxDuration: Duration{1 * time.Minute}, ContinuousProfilerFrequency: Duration{2 * time.Minute}},
			false,
		},
		{
			"integer durations parsed",
			[]byte(fmt.Sprintf(`{"api-max-duration": "%v", "continuous-profiler-frequency": "%v"}`, 1*time.Minute, 2*time.Minute)),
			Config{APIMaxDuration: Duration{1 * time.Minute}, ContinuousProfilerFrequency: Duration{2 * time.Minute}},
			false,
		},
		{
			"nanosecond durations parsed",
			[]byte(`{"api-max-duration": 5000000000, "continuous-profiler-frequency": 5000000000}`),
			Config{APIMaxDuration: Duration{5 * time.Second}, ContinuousProfilerFrequency: Duration{5 * time.Second}},
			false,
		},
		{
			"bad durations",
			[]byte(`{"api-max-duration": "bad-duration"}`),
			Config{},
			true,
		},

		{
			"tx pool configurations",
			[]byte(`{"tx-pool-price-limit": 1, "tx-pool-price-bump": 2, "tx-pool-account-slots": 3, "tx-pool-global-slots": 4, "tx-pool-account-queue": 5, "tx-pool-global-queue": 6}`),
			Config{
				TxPoolPriceLimit:   1,
				TxPoolPriceBump:    2,
				TxPoolAccountSlots: 3,
				TxPoolGlobalSlots:  4,
				TxPoolAccountQueue: 5,
				TxPoolGlobalQueue:  6,
			},
			false,
		},

		{
			"state sync enabled",
			[]byte(`{"state-sync-enabled":true}`),
			Config{StateSyncEnabled: newTrue()},
			false,
		},
		{
			"state sync sources",
			[]byte(`{"state-sync-ids": "NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat"}`),
			Config{StateSyncIDs: "NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat"},
			false,
		},
		{
			"empty transaction history ",
			[]byte(`{}`),
			Config{TransactionHistory: 0},
			false,
		},
		{
			"zero transaction history",
			[]byte(`{"transaction-history": 0}`),
			func() Config {
				return Config{TransactionHistory: 0}
			}(),
			false,
		},
		{
			"1 transaction history",
			[]byte(`{"transaction-history": 1}`),
			func() Config {
				return Config{TransactionHistory: 1}
			}(),
			false,
		},
		{
			"-1 transaction history",
			[]byte(`{"transaction-history": -1}`),
			Config{},
			true,
		},
		{
			"allow unprotected tx hashes",
			[]byte(`{"allow-unprotected-tx-hashes": ["0x803351deb6d745e91545a6a3e1c0ea3e9a6a02a1a4193b70edfcd2f40f71a01c"]}`),
			Config{AllowUnprotectedTxHashes: []common.Hash{common.HexToHash("0x803351deb6d745e91545a6a3e1c0ea3e9a6a02a1a4193b70edfcd2f40f71a01c")}},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tmp Config
			err := json.Unmarshal(tt.givenJSON, &tmp)
			if tt.expectedErr {
				require.Error(t, err) //nolint:forbidigo // uses standard library
			} else {
				require.NoError(t, err)
				tmp.deprecate()
				require.Equal(t, tt.expected, tmp)
			}
		})
	}
}

func TestGetConfig(t *testing.T) {
	tests := []struct {
		name       string
		configJSON []byte
		networkID  uint32
		expected   func(*testing.T, Config)
	}{
		{
			name:       "custom config values",
			configJSON: []byte(`{"rpc-tx-fee-cap": 11,"eth-apis": ["debug"]}`),
			networkID:  constants.TestnetID,
			expected: func(t *testing.T, config Config) {
				require.Equal(t, float64(11), config.RPCTxFeeCap, "Tx Fee Cap should be set")
				require.Equal(t, []string{"debug"}, config.EthAPIs(), "EnabledEthAPIs should be set")
			},
		},
		{
			name:       "partial config with defaults",
			configJSON: []byte(`{"rpc-tx-fee-cap": 11,"eth-apis": ["debug"], "tx-pool-price-limit": 100}`),
			networkID:  constants.TestnetID,
			expected: func(t *testing.T, config Config) {
				defaultConfig := NewDefaultConfig()
				require.Equal(t, defaultConfig.PriceOptionMaxTip, config.PriceOptionMaxTip)
				require.Equal(t, float64(11), config.RPCTxFeeCap)
				require.Equal(t, []string{"debug"}, config.EthAPIs())
				require.Equal(t, uint64(100), config.TxPoolPriceLimit)
			},
		},
		{
			name:       "nil config uses defaults",
			configJSON: nil,
			networkID:  constants.TestnetID,
			expected: func(t *testing.T, config Config) {
				defaultConfig := NewDefaultConfig()
				require.Equal(t, defaultConfig, config)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, _, err := GetConfig(tt.configJSON, tt.networkID)
			require.NoError(t, err)
			tt.expected(t, config)
		})
	}
}
