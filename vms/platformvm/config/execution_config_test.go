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
				"push-gossip-num-validators": 3,
				"push-gossip-num-peers": 4,
				"push-regossip-num-validators": 5,
				"push-regossip-num-peers": 6,
				"push-gossip-discarded-cache-size": 7,
				"push-gossip-max-regossip-frequency": 8,
				"push-gossip-frequency": 9,
				"pull-gossip-poll-size": 10,
				"pull-gossip-frequency": 11,
				"pull-gossip-throttling-period": 12,
				"pull-gossip-throttling-limit": 13,
				"expected-bloom-filter-elements": 14,
				"expected-bloom-filter-false-positive-probability": 15,
				"max-bloom-filter-false-positive-probability": 16
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
				PushGossipNumValidators:                     3,
				PushGossipNumPeers:                          4,
				PushRegossipNumValidators:                   5,
				PushRegossipNumPeers:                        6,
				PushGossipDiscardedCacheSize:                7,
				PushGossipMaxRegossipFrequency:              8,
				PushGossipFrequency:                         9,
				PullGossipPollSize:                          10,
				PullGossipFrequency:                         11,
				PullGossipThrottlingPeriod:                  12,
				PullGossipThrottlingLimit:                   13,
				ExpectedBloomFilterElements:                 14,
				ExpectedBloomFilterFalsePositiveProbability: 15,
				MaxBloomFilterFalsePositiveProbability:      16,
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
				"push-gossip-discarded-cache-size": 1024,
				"push-gossip-max-regossip-frequency": 10000000000,
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
				PushGossipNumValidators:                     DefaultExecutionConfig.Network.PushGossipNumValidators,
				PushGossipNumPeers:                          DefaultExecutionConfig.Network.PushGossipNumPeers,
				PushRegossipNumValidators:                   DefaultExecutionConfig.Network.PushRegossipNumValidators,
				PushRegossipNumPeers:                        DefaultExecutionConfig.Network.PushRegossipNumPeers,
				PushGossipDiscardedCacheSize:                1024,
				PushGossipMaxRegossipFrequency:              10 * time.Second,
				PushGossipFrequency:                         DefaultExecutionConfig.Network.PushGossipFrequency,
				PullGossipPollSize:                          3,
				PullGossipFrequency:                         4,
				PullGossipThrottlingPeriod:                  5,
				PullGossipThrottlingLimit:                   DefaultExecutionConfig.Network.PullGossipThrottlingLimit,
				ExpectedBloomFilterElements:                 DefaultExecutionConfig.Network.ExpectedBloomFilterElements,
				ExpectedBloomFilterFalsePositiveProbability: DefaultExecutionConfig.Network.ExpectedBloomFilterFalsePositiveProbability,
				MaxBloomFilterFalsePositiveProbability:      DefaultExecutionConfig.Network.MaxBloomFilterFalsePositiveProbability,
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
