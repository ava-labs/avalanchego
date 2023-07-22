// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecutionConfigUnmarshal(t *testing.T) {
	t.Run("default values from empty json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{}`)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		expected := ExecutionConfig{
			BlockCacheSize:               blockCacheSize,
			TxCacheSize:                  txCacheSize,
			TransformedSubnetTxCacheSize: transformedSubnetTxCacheSize,
			ValidatorDiffsCacheSize:      validatorDiffsCacheSize,
			RewardUTXOsCacheSize:         rewardUTXOsCacheSize,
			ChainCacheSize:               chainCacheSize,
			ChainDBCacheSize:             chainDBCacheSize,
			ChecksumsEnabled:             checksumsEnabled,
		}
		require.Equal(expected, *ec)
	})

	t.Run("default values from empty bytes", func(t *testing.T) {
		require := require.New(t)
		b := []byte(``)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		expected := ExecutionConfig{
			BlockCacheSize:               blockCacheSize,
			TxCacheSize:                  txCacheSize,
			TransformedSubnetTxCacheSize: transformedSubnetTxCacheSize,
			ValidatorDiffsCacheSize:      validatorDiffsCacheSize,
			RewardUTXOsCacheSize:         rewardUTXOsCacheSize,
			ChainCacheSize:               chainCacheSize,
			ChainDBCacheSize:             chainDBCacheSize,
			ChecksumsEnabled:             checksumsEnabled,
		}
		require.Equal(expected, *ec)
	})

	t.Run("mix default and extracted values from json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{"block-cache-size":1}`)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		expected := ExecutionConfig{
			BlockCacheSize:               1,
			TxCacheSize:                  txCacheSize,
			TransformedSubnetTxCacheSize: transformedSubnetTxCacheSize,
			ValidatorDiffsCacheSize:      validatorDiffsCacheSize,
			RewardUTXOsCacheSize:         rewardUTXOsCacheSize,
			ChainCacheSize:               chainCacheSize,
			ChainDBCacheSize:             chainDBCacheSize,
			ChecksumsEnabled:             checksumsEnabled,
		}
		require.Equal(expected, *ec)
	})

	t.Run("all values extracted from json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{
			"block-cache-size":1,
			"tx-cache-size":2,
			"transformed-subnet-tx-cache-size":3,
			"validator-diffs-cache-size": 4,
			"reward-utxos-cache-size": 5,
			"chain-cache-size": 6,
			"chain-db-cache-size": 7,
			"checksums-enabled": true
		}`)
		ec, err := GetExecutionConfig(b)
		require.NoError(err)
		expected := ExecutionConfig{
			BlockCacheSize:               1,
			TxCacheSize:                  2,
			TransformedSubnetTxCacheSize: 3,
			ValidatorDiffsCacheSize:      4,
			RewardUTXOsCacheSize:         5,
			ChainCacheSize:               6,
			ChainDBCacheSize:             7,
			ChecksumsEnabled:             true,
		}
		require.Equal(expected, *ec)
	})
}
