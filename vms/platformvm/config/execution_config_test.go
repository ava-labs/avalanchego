// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/platformvm/network"
)

func TestExecutionConfigUnmarshal(t *testing.T) {
	t.Run("default values from empty json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{}`)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		require.Equal(&DefaultExecutionConfig, ec)
	})

	t.Run("default values from empty bytes", func(t *testing.T) {
		require := require.New(t)
		b := []byte(``)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		require.Equal(&DefaultExecutionConfig, ec)
	})

	t.Run("mix default and extracted values from json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{"block-cache-size":1}`)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		expected := DefaultExecutionConfig
		expected.BlockCacheSize = 1
		require.Equal(&expected, ec)
	})

	t.Run("all values extracted from json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{
			"network": {
				"max-validator-set-staleness": 1,
				"target-gossip-size": 2,
				"pull-gossip-poll-size": 3,
				"pull-gossip-frequency": 4,
				"pull-gossip-throttling-period": 5,
				"pull-gossip-throttling-limit": 6,
				"expected-bloom-filter-elements":7,
				"expected-bloom-filter-false-positive-probability": 8,
				"max-bloom-filter-false-positive-probability": 9,
				"legacy-push-gossip-cache-size": 10
			},
			"block-cache-size": 1,
			"tx-cache-size": 2,
			"transformed-subnet-tx-cache-size": 3,
			"reward-utxos-cache-size": 5,
			"chain-cache-size": 6,
			"chain-db-cache-size": 7,
			"block-id-cache-size": 8,
			"fx-owner-cache-size": 9,
			"checksums-enabled": true,
			"mempool-prune-frequency": 60000000000
		}`)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		expected := &ExecutionConfig{
			Network: network.Config{
				MaxValidatorSetStaleness:                    1,
				TargetGossipSize:                            2,
				PullGossipPollSize:                          3,
				PullGossipFrequency:                         4,
				PullGossipThrottlingPeriod:                  5,
				PullGossipThrottlingLimit:                   6,
				ExpectedBloomFilterElements:                 7,
				ExpectedBloomFilterFalsePositiveProbability: 8,
				MaxBloomFilterFalsePositiveProbability:      9,
				LegacyPushGossipCacheSize:                   10,
			},
			BlockCacheSize:               1,
			TxCacheSize:                  2,
			TransformedSubnetTxCacheSize: 3,
			RewardUTXOsCacheSize:         5,
			ChainCacheSize:               6,
			ChainDBCacheSize:             7,
			BlockIDCacheSize:             8,
			FxOwnerCacheSize:             9,
			ChecksumsEnabled:             true,
			MempoolPruneFrequency:        time.Minute,
		}
		require.Equal(expected, ec)
	})

	t.Run("default values applied correctly", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{
			"network": {
				"max-validator-set-staleness": 1,
				"target-gossip-size": 2,
				"pull-gossip-poll-size": 3,
				"pull-gossip-frequency": 4,
				"pull-gossip-throttling-period": 5
			},
			"block-cache-size": 1,
			"tx-cache-size": 2,
			"transformed-subnet-tx-cache-size": 3,
			"reward-utxos-cache-size": 5,
			"chain-cache-size": 6,
			"chain-db-cache-size": 7,
			"block-id-cache-size": 8,
			"fx-owner-cache-size": 9,
			"checksums-enabled": true
		}`)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		expected := &ExecutionConfig{
			Network: network.Config{
				MaxValidatorSetStaleness:                    1,
				TargetGossipSize:                            2,
				PullGossipPollSize:                          3,
				PullGossipFrequency:                         4,
				PullGossipThrottlingPeriod:                  5,
				PullGossipThrottlingLimit:                   DefaultExecutionConfig.Network.PullGossipThrottlingLimit,
				ExpectedBloomFilterElements:                 DefaultExecutionConfig.Network.ExpectedBloomFilterElements,
				ExpectedBloomFilterFalsePositiveProbability: DefaultExecutionConfig.Network.ExpectedBloomFilterFalsePositiveProbability,
				MaxBloomFilterFalsePositiveProbability:      DefaultExecutionConfig.Network.MaxBloomFilterFalsePositiveProbability,
				LegacyPushGossipCacheSize:                   DefaultExecutionConfig.Network.LegacyPushGossipCacheSize,
			},
			BlockCacheSize:               1,
			TxCacheSize:                  2,
			TransformedSubnetTxCacheSize: 3,
			RewardUTXOsCacheSize:         5,
			ChainCacheSize:               6,
			ChainDBCacheSize:             7,
			BlockIDCacheSize:             8,
			FxOwnerCacheSize:             9,
			ChecksumsEnabled:             true,
			MempoolPruneFrequency:        30 * time.Minute,
		}
		require.Equal(expected, ec)
	})
}
