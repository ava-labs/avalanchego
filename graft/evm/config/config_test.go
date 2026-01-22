// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// unmarshalTest defines a test case for JSON unmarshaling.
type unmarshalTest[T any] struct {
	name      string
	givenJSON []byte
	expected  T
}

// commonUnmarshalTests returns test cases for fields in CommonConfig.
// These tests apply to both CChainConfig and L1Config.
func commonUnmarshalTests[T any](makeConfig func(CommonConfig) T) []unmarshalTest[T] {
	return []unmarshalTest[T]{
		{
			name:      "string durations parsed",
			givenJSON: []byte(`{"api-max-duration": "1m", "continuous-profiler-frequency": "2m"}`),
			expected: makeConfig(CommonConfig{
				APIMaxDuration:              Duration{1 * time.Minute},
				ContinuousProfilerFrequency: Duration{2 * time.Minute},
			}),
		},
		{
			name:      "integer durations parsed",
			givenJSON: []byte(fmt.Sprintf(`{"api-max-duration": "%v", "continuous-profiler-frequency": "%v"}`, 1*time.Minute, 2*time.Minute)),
			expected: makeConfig(CommonConfig{
				APIMaxDuration:              Duration{1 * time.Minute},
				ContinuousProfilerFrequency: Duration{2 * time.Minute},
			}),
		},
		{
			name:      "nanosecond durations parsed",
			givenJSON: []byte(`{"api-max-duration": 5000000000, "continuous-profiler-frequency": 5000000000}`),
			expected: makeConfig(CommonConfig{
				APIMaxDuration:              Duration{5 * time.Second},
				ContinuousProfilerFrequency: Duration{5 * time.Second},
			}),
		},
		{
			name:      "tx pool configurations",
			givenJSON: []byte(`{"tx-pool-price-limit": 1, "tx-pool-price-bump": 2, "tx-pool-account-slots": 3, "tx-pool-global-slots": 4, "tx-pool-account-queue": 5, "tx-pool-global-queue": 6}`),
			expected: makeConfig(CommonConfig{
				TxPoolPriceLimit:   1,
				TxPoolPriceBump:    2,
				TxPoolAccountSlots: 3,
				TxPoolGlobalSlots:  4,
				TxPoolAccountQueue: 5,
				TxPoolGlobalQueue:  6,
			}),
		},
		{
			name:      "state sync enabled",
			givenJSON: []byte(`{"state-sync-enabled":true}`),
			expected:  makeConfig(CommonConfig{StateSyncEnabled: utils.PointerTo(true)}),
		},
		{
			name:      "state sync sources",
			givenJSON: []byte(`{"state-sync-ids": "NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat"}`),
			expected:  makeConfig(CommonConfig{StateSyncIDs: "NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat"}),
		},
		{
			name:      "empty transaction history",
			givenJSON: []byte(`{}`),
			expected:  makeConfig(CommonConfig{TransactionHistory: 0}),
		},
		{
			name:      "zero transaction history",
			givenJSON: []byte(`{"transaction-history": 0}`),
			expected:  makeConfig(CommonConfig{TransactionHistory: 0}),
		},
		{
			name:      "1 transaction history",
			givenJSON: []byte(`{"transaction-history": 1}`),
			expected:  makeConfig(CommonConfig{TransactionHistory: 1}),
		},
		{
			name:      "allow unprotected tx hashes",
			givenJSON: []byte(`{"allow-unprotected-tx-hashes": ["0x803351deb6d745e91545a6a3e1c0ea3e9a6a02a1a4193b70edfcd2f40f71a01c"]}`),
			expected: makeConfig(CommonConfig{
				AllowUnprotectedTxHashes: []common.Hash{common.HexToHash("0x803351deb6d745e91545a6a3e1c0ea3e9a6a02a1a4193b70edfcd2f40f71a01c")},
			}),
		},
	}
}

func TestUnmarshalCChainConfig(t *testing.T) {
	// Test common fields
	commonTests := commonUnmarshalTests(func(c CommonConfig) CChainConfig {
		return CChainConfig{CommonConfig: c}
	})
	for _, tt := range commonTests {
		t.Run(tt.name, func(t *testing.T) {
			var tmp CChainConfig
			err := json.Unmarshal(tt.givenJSON, &tmp)
			require.NoError(t, err)
			tmp.deprecate()
			require.Equal(t, tt.expected, tmp)
		})
	}

	// Test C-Chain specific fields
	t.Run("price option settings", func(t *testing.T) {
		var tmp CChainConfig
		err := json.Unmarshal([]byte(`{"price-options-slow-fee-percentage": 90, "price-options-fast-fee-percentage": 110, "price-options-max-tip": 1000}`), &tmp)
		require.NoError(t, err)
		require.Equal(t, uint64(90), tmp.PriceOptionSlowFeePercentage)
		require.Equal(t, uint64(110), tmp.PriceOptionFastFeePercentage)
		require.Equal(t, uint64(1000), tmp.PriceOptionMaxTip)
	})
}

func TestUnmarshalL1Config(t *testing.T) {
	// Test common fields
	commonTests := commonUnmarshalTests(func(c CommonConfig) L1Config {
		return L1Config{CommonConfig: c}
	})
	for _, tt := range commonTests {
		t.Run(tt.name, func(t *testing.T) {
			var tmp L1Config
			err := json.Unmarshal(tt.givenJSON, &tmp)
			require.NoError(t, err)
			tmp.deprecate()
			require.Equal(t, tt.expected, tmp)
		})
	}

	// Test L1 specific fields
	t.Run("airdrop file", func(t *testing.T) {
		var tmp L1Config
		err := json.Unmarshal([]byte(`{"airdrop": "/path/to/airdrop.json"}`), &tmp)
		require.NoError(t, err)
		require.Equal(t, "/path/to/airdrop.json", tmp.AirdropFile)
	})

	t.Run("validators api enabled", func(t *testing.T) {
		var tmp L1Config
		err := json.Unmarshal([]byte(`{"validators-api-enabled": true}`), &tmp)
		require.NoError(t, err)
		require.True(t, tmp.ValidatorsAPIEnabled)
	})

	t.Run("fee recipient", func(t *testing.T) {
		var tmp L1Config
		err := json.Unmarshal([]byte(`{"feeRecipient": "0x1234567890123456789012345678901234567890"}`), &tmp)
		require.NoError(t, err)
		require.Equal(t, "0x1234567890123456789012345678901234567890", tmp.FeeRecipient)
	})
}

func TestGetCChainConfig(t *testing.T) {
	tests := []struct {
		name       string
		configJSON []byte
		networkID  uint32
		expected   func(*testing.T, CChainConfig)
	}{
		{
			name:       "custom config values",
			configJSON: []byte(`{"rpc-tx-fee-cap": 11,"eth-apis": ["debug"]}`),
			networkID:  constants.TestnetID,
			expected: func(t *testing.T, config CChainConfig) {
				require.Equal(t, float64(11), config.RPCTxFeeCap)
				require.Equal(t, []string{"debug"}, config.EthAPIs())
			},
		},
		{
			name:       "partial config with defaults",
			configJSON: []byte(`{"rpc-tx-fee-cap": 11,"eth-apis": ["debug"], "tx-pool-price-limit": 100}`),
			networkID:  constants.TestnetID,
			expected: func(t *testing.T, config CChainConfig) {
				defaultConfig := NewDefaultCChainConfig()
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
			expected: func(t *testing.T, config CChainConfig) {
				defaultConfig := NewDefaultCChainConfig()
				require.Equal(t, defaultConfig, config)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, _, err := GetConfig(tt.configJSON, tt.networkID, NewDefaultCChainConfig)
			require.NoError(t, err)
			tt.expected(t, config)
		})
	}
}

func TestGetL1Config(t *testing.T) {
	tests := []struct {
		name       string
		configJSON []byte
		networkID  uint32
		expected   func(*testing.T, L1Config)
	}{
		{
			name:       "custom config values",
			configJSON: []byte(`{"rpc-tx-fee-cap": 11,"eth-apis": ["debug"]}`),
			networkID:  constants.TestnetID,
			expected: func(t *testing.T, config L1Config) {
				require.Equal(t, float64(11), config.RPCTxFeeCap)
				require.Equal(t, []string{"debug"}, config.EthAPIs())
			},
		},
		{
			name:       "partial config with defaults",
			configJSON: []byte(`{"rpc-tx-fee-cap": 11,"eth-apis": ["debug"], "tx-pool-price-limit": 100}`),
			networkID:  constants.TestnetID,
			expected: func(t *testing.T, config L1Config) {
				defaultConfig := NewDefaultL1Config()
				require.Equal(t, defaultConfig.ValidatorsAPIEnabled, config.ValidatorsAPIEnabled)
				require.Equal(t, float64(11), config.RPCTxFeeCap)
				require.Equal(t, []string{"debug"}, config.EthAPIs())
				require.Equal(t, uint64(100), config.TxPoolPriceLimit)
			},
		},
		{
			name:       "nil config uses defaults",
			configJSON: nil,
			networkID:  constants.TestnetID,
			expected: func(t *testing.T, config L1Config) {
				defaultConfig := NewDefaultL1Config()
				require.Equal(t, defaultConfig, config)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, _, err := GetConfig(tt.configJSON, tt.networkID, NewDefaultL1Config)
			require.NoError(t, err)
			tt.expected(t, config)
		})
	}
}

// TestCommonValidation verifies that both CChainConfig and L1Config reject the same invalid
// common configuration inputs. This test ensures the common validation logic stays in sync.
func TestCommonValidation(t *testing.T) {
	tests := []struct {
		name       string
		configJSON []byte
		wantErr    error
	}{
		{
			name:       "populate missing tries with pruning enabled",
			configJSON: []byte(`{"populate-missing-tries": 100, "pruning-enabled": true}`),
			wantErr:    ErrPopulateMissingTriesWithPruning,
		},
		{
			name:       "populate missing tries with offline pruning enabled",
			configJSON: []byte(`{"populate-missing-tries": 100, "offline-pruning-enabled": true, "pruning-enabled": true}`),
			wantErr:    ErrPopulateMissingTriesWithPruning,
		},
		{
			name:       "populate missing tries with zero parallelism",
			configJSON: []byte(`{"populate-missing-tries": 100, "populate-missing-tries-parallelism": 0, "pruning-enabled": false}`),
			wantErr:    ErrPopulateMissingTriesNoReader,
		},
		{
			name:       "offline pruning without pruning",
			configJSON: []byte(`{"offline-pruning-enabled": true, "pruning-enabled": false}`),
			wantErr:    ErrOfflinePruningWithoutPruning,
		},
		{
			name:       "pruning with zero commit interval",
			configJSON: []byte(`{"pruning-enabled": true, "commit-interval": 0, "state-history": 32}`),
			wantErr:    ErrPruningZeroCommitInterval,
		},
		{
			name:       "pruning with zero state history",
			configJSON: []byte(`{"pruning-enabled": true, "state-history": 0}`),
			wantErr:    ErrPruningZeroStateHistory,
		},
		{
			name:       "push gossip percent stake below zero",
			configJSON: []byte(`{"push-gossip-percent-stake": -0.1}`),
			wantErr:    ErrPushGossipPercentStakeOutOfRange,
		},
		{
			name:       "push gossip percent stake above one",
			configJSON: []byte(`{"push-gossip-percent-stake": 1.5}`),
			wantErr:    ErrPushGossipPercentStakeOutOfRange,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" (CChainConfig)", func(t *testing.T) {
			_, _, err := GetConfig(tt.configJSON, constants.LocalID, NewDefaultCChainConfig)
			require.ErrorIs(t, err, tt.wantErr)
		})

		t.Run(tt.name+" (L1Config)", func(t *testing.T) {
			_, _, err := GetConfig(tt.configJSON, constants.LocalID, NewDefaultL1Config)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
