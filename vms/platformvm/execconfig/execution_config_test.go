package execconfig

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
		expected := Config{
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
		expected := Config{
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
		expected := Config{
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
