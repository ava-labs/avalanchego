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
		}
		require.Equal(expected, *ec)
	})

	t.Run("mix default and extracted values from json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{"blockCacheSize":1}`)
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
		}
		require.Equal(expected, *ec)
	})
}
