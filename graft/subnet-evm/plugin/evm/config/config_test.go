// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/pebbledb"
	"github.com/ava-labs/avalanchego/graft/evm/config"
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

	// Test Subnet-EVM specific fields
	t.Run("database type", func(t *testing.T) {
		var got Config
		err := json.Unmarshal([]byte(`{"database-type": "leveldb"}`), &got)
		require.NoError(t, err)
		require.Equal(t, "leveldb", got.DatabaseType)
	})

	t.Run("validators api enabled", func(t *testing.T) {
		var got Config
		err := json.Unmarshal([]byte(`{"validators-api-enabled": false}`), &got)
		require.NoError(t, err)
		require.False(t, got.ValidatorsAPIEnabled)
	})

	t.Run("database path", func(t *testing.T) {
		var got Config
		err := json.Unmarshal([]byte(`{"database-path": "/custom/path"}`), &got)
		require.NoError(t, err)
		require.Equal(t, "/custom/path", got.DatabasePath)
	})
}

func TestUnmarshalConfigErrors(t *testing.T) {
	t.Run("bad durations", func(t *testing.T) {
		var got Config
		err := json.Unmarshal([]byte(`{"api-max-duration": "bad-duration"}`), &got)
		require.ErrorIs(t, err, config.ErrInvalidDuration)
	})

	t.Run("negative transaction history", func(t *testing.T) {
		var got Config
		err := json.Unmarshal([]byte(`{"transaction-history": -1}`), &got)
		var target *json.UnmarshalTypeError
		require.ErrorAs(t, err, &target)
	})
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
			configJSON: []byte(`{"rpc-tx-fee-cap": 11, "eth-apis": ["debug"], "database-type": "leveldb"}`),
			networkID:  constants.TestnetID,
			check: func(t *testing.T, got Config) {
				require.Equal(t, float64(11), got.RPCTxFeeCap)
				require.Equal(t, []string{"debug"}, got.EnabledEthAPIs)
				require.Equal(t, "leveldb", got.DatabaseType)
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
				require.Equal(t, pebbledb.Name, got.DatabaseType)
				require.True(t, got.ValidatorsAPIEnabled)
				require.NotNil(t, got.StateSyncEnabled)
				require.False(t, *got.StateSyncEnabled)
			},
		},
		{
			name:       "nil config uses defaults",
			configJSON: nil,
			networkID:  constants.TestnetID,
			check: func(t *testing.T, got Config) {
				require.Equal(t, pebbledb.Name, got.DatabaseType)
				require.True(t, got.ValidatorsAPIEnabled)
				require.NotNil(t, got.StateSyncEnabled)
				require.False(t, *got.StateSyncEnabled)
				require.Equal(t, float64(100), got.RPCTxFeeCap)
				require.Equal(t, uint64(50_000_000), got.RPCGasCap)
			},
		},
		{
			name:       "user values override defaults",
			configJSON: []byte(`{"database-type": "leveldb", "validators-api-enabled": false, "state-sync-enabled": true, "rpc-tx-fee-cap": 5}`),
			networkID:  constants.TestnetID,
			check: func(t *testing.T, got Config) {
				require.Equal(t, "leveldb", got.DatabaseType)
				require.False(t, got.ValidatorsAPIEnabled)
				require.NotNil(t, got.StateSyncEnabled)
				require.True(t, *got.StateSyncEnabled)
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
	t.Run("negative transaction history rejected", func(t *testing.T) {
		_, _, err := GetConfig([]byte(`{"transaction-history": -1}`), constants.LocalID)
		var target *json.UnmarshalTypeError
		require.ErrorAs(t, err, &target)
	})

	t.Run("valid config accepted", func(t *testing.T) {
		_, _, err := GetConfig([]byte(`{"database-type": "leveldb"}`), constants.LocalID)
		require.NoError(t, err)
	})
}

func TestStateSyncEnabledDefaults(t *testing.T) {
	t.Run("nil defaults to disabled for subnet-evm", func(t *testing.T) {
		got, _, err := GetConfig([]byte(`{}`), constants.TestnetID)
		require.NoError(t, err)
		require.NotNil(t, got.StateSyncEnabled)
		require.False(t, *got.StateSyncEnabled)
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
	require.True(t, got.ValidatorsAPIEnabled, "ValidatorsAPIEnabled should default to true")

	gotDisabled, _, err := GetConfig([]byte(`{"pruning-enabled": false, "metrics-expensive-enabled": false, "validators-api-enabled": false}`), constants.TestnetID)
	require.NoError(t, err)
	require.False(t, gotDisabled.Pruning)
	require.False(t, gotDisabled.MetricsExpensiveEnabled)
	require.False(t, gotDisabled.ValidatorsAPIEnabled)
}

func TestDurationFields(t *testing.T) {
	got, _, err := GetConfig([]byte(`{"continuous-profiler-frequency": "5m"}`), constants.TestnetID)
	require.NoError(t, err)
	require.Equal(t, 5*time.Minute, got.ContinuousProfilerFrequency.Duration)

	gotDefault, _, err := GetConfig(nil, constants.TestnetID)
	require.NoError(t, err)
	require.Equal(t, 15*time.Minute, gotDefault.ContinuousProfilerFrequency.Duration)
}

func TestDatabaseDefaults(t *testing.T) {
	got, _, err := GetConfig(nil, constants.TestnetID)
	require.NoError(t, err)
	require.Equal(t, pebbledb.Name, got.DatabaseType)

	gotCustom, _, err := GetConfig([]byte(`{"database-type": "leveldb"}`), constants.TestnetID)
	require.NoError(t, err)
	require.Equal(t, "leveldb", gotCustom.DatabaseType)
}
