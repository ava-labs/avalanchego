// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/config"
	evmutils "github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestUnmarshalConfig(t *testing.T) {
	// Test common base config fields
	commonTests := config.CommonUnmarshalTests(func(c config.BaseConfig) Config {
		return Config{BaseConfig: c}
	})
	for _, tt := range commonTests {
		t.Run(tt.Name, func(t *testing.T) {
			var got Config
			err := json.Unmarshal(tt.GivenJSON, &got)
			require.NoError(t, err)
			require.Equal(t, tt.Want, got)
		})
	}

	// Test C-Chain specific fields
	t.Run("price option configurations", func(t *testing.T) {
		var got Config
		err := json.Unmarshal([]byte(`{"price-options-slow-fee-percentage": 85, "price-options-fast-fee-percentage": 115, "price-options-max-tip": 5000000000}`), &got)
		require.NoError(t, err)
		require.Equal(t, uint64(85), got.PriceOptionSlowFeePercentage)
		require.Equal(t, uint64(115), got.PriceOptionFastFeePercentage)
		require.Equal(t, uint64(5000000000), got.PriceOptionMaxTip)
	})
}

func TestUnmarshalConfigErrors(t *testing.T) {
	tests := []struct {
		name      string
		givenJSON []byte
	}{
		{
			name:      "bad durations",
			givenJSON: []byte(`{"api-max-duration": "bad-duration"}`),
		},
		{
			name:      "negative transaction history",
			givenJSON: []byte(`{"transaction-history": -1}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Config
			err := json.Unmarshal(tt.givenJSON, &got)
			require.Error(t, err)
		})
	}
}

func TestGetConfig(t *testing.T) {
	tests := []struct {
		name       string
		configJSON []byte
		networkID  uint32
		check      func(*testing.T, Config)
	}{
		{
			name:       "custom config values",
			configJSON: []byte(`{"rpc-tx-fee-cap": 11, "eth-apis": ["debug"], "price-options-max-tip": 50000000000}`),
			networkID:  constants.TestnetID,
			check: func(t *testing.T, got Config) {
				require.Equal(t, float64(11), got.RPCTxFeeCap)
				require.Equal(t, []string{"debug"}, got.EnabledEthAPIs)
				require.Equal(t, uint64(50000000000), got.PriceOptionMaxTip)
			},
		},
		{
			name:       "partial config with defaults",
			configJSON: []byte(`{"rpc-tx-fee-cap": 11, "eth-apis": ["debug"], "tx-pool-price-limit": 100}`),
			networkID:  constants.TestnetID,
			check: func(t *testing.T, got Config) {
				require.Equal(t, float64(11), got.RPCTxFeeCap)
				require.Equal(t, []string{"debug"}, got.EnabledEthAPIs)
				require.Equal(t, uint64(100), got.TxPoolPriceLimit)
				require.Equal(t, uint64(20*evmutils.GWei), got.PriceOptionMaxTip)
				require.Equal(t, uint64(95), got.PriceOptionSlowFeePercentage)
				require.Equal(t, uint64(105), got.PriceOptionFastFeePercentage)
			},
		},
		{
			name:       "nil config uses defaults",
			configJSON: nil,
			networkID:  constants.TestnetID,
			check: func(t *testing.T, got Config) {
				require.Equal(t, uint64(20*evmutils.GWei), got.PriceOptionMaxTip)
				require.Equal(t, uint64(95), got.PriceOptionSlowFeePercentage)
				require.Equal(t, uint64(105), got.PriceOptionFastFeePercentage)
				require.Equal(t, float64(100), got.RPCTxFeeCap)
				require.Equal(t, uint64(50_000_000), got.RPCGasCap)
			},
		},
		{
			name:       "user values override defaults",
			configJSON: []byte(`{"price-options-max-tip": 1, "price-options-slow-fee-percentage": 50, "rpc-tx-fee-cap": 5}`),
			networkID:  constants.TestnetID,
			check: func(t *testing.T, got Config) {
				require.Equal(t, uint64(1), got.PriceOptionMaxTip)
				require.Equal(t, uint64(50), got.PriceOptionSlowFeePercentage)
				require.Equal(t, float64(5), got.RPCTxFeeCap)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := GetConfig(tt.configJSON, tt.networkID)
			require.NoError(t, err)
			tt.check(t, got)
		})
	}
}

func TestGetConfigValidation(t *testing.T) {
	t.Run("production network rejects custom commit interval", func(t *testing.T) {
		_, _, err := GetConfig([]byte(`{"commit-interval": 100}`), constants.MainnetID)
		require.Error(t, err)
	})

	t.Run("local network allows custom commit interval", func(t *testing.T) {
		_, _, err := GetConfig([]byte(`{"commit-interval": 100}`), constants.LocalID)
		require.NoError(t, err)
	})

	t.Run("negative transaction history rejected", func(t *testing.T) {
		_, _, err := GetConfig([]byte(`{"transaction-history": -1}`), constants.LocalID)
		require.Error(t, err)
	})
}

func TestStateSyncEnabledPointer(t *testing.T) {
	t.Run("nil defaults to genesis activation", func(t *testing.T) {
		got, _, err := GetConfig([]byte(`{}`), constants.TestnetID)
		require.NoError(t, err)
		require.Nil(t, got.StateSyncEnabled)
	})

	t.Run("explicitly enabled", func(t *testing.T) {
		got, _, err := GetConfig([]byte(`{"state-sync-enabled": true}`), constants.TestnetID)
		require.NoError(t, err)
		require.NotNil(t, got.StateSyncEnabled)
		require.True(t, *got.StateSyncEnabled)
	})

	t.Run("explicitly disabled", func(t *testing.T) {
		got, _, err := GetConfig([]byte(`{"state-sync-enabled": false}`), constants.TestnetID)
		require.NoError(t, err)
		require.NotNil(t, got.StateSyncEnabled)
		require.False(t, *got.StateSyncEnabled)
	})
}

func TestBoolDefaultsWithTrueValue(t *testing.T) {
	got, _, err := GetConfig(nil, constants.TestnetID)
	require.NoError(t, err)
	require.True(t, got.Pruning, "Pruning should default to true")
	require.True(t, got.MetricsExpensiveEnabled, "MetricsExpensiveEnabled should default to true")

	gotDisabled, _, err := GetConfig([]byte(`{"pruning-enabled": false, "metrics-expensive-enabled": false}`), constants.TestnetID)
	require.NoError(t, err)
	require.False(t, gotDisabled.Pruning)
	require.False(t, gotDisabled.MetricsExpensiveEnabled)
}

func TestDurationFields(t *testing.T) {
	got, _, err := GetConfig([]byte(`{"continuous-profiler-frequency": "5m"}`), constants.TestnetID)
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, got.ContinuousProfilerFrequency.Duration)

	gotDefault, _, err := GetConfig(nil, constants.TestnetID)
	require.NoError(t, err)
	require.Equal(t, 15*time.Minute, gotDefault.ContinuousProfilerFrequency.Duration)
}
