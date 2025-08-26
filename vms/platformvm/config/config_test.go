// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Requires all values in a struct to be initialized
func verifyInitializedStruct(tb testing.TB, s interface{}) {
	tb.Helper()

	require := require.New(tb)

	structType := reflect.TypeOf(s)
	require.Equal(reflect.Struct, structType.Kind())

	v := reflect.ValueOf(s)
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		require.True(field.IsValid(), "invalid field: ", structType.Field(i).Name)
		require.False(field.IsZero(), "zero field: ", structType.Field(i).Name)
	}
}

func TestConfigUnmarshal(t *testing.T) {
	t.Run("default values from empty json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{}`)
		ec, err := GetConfig(b)
		require.NoError(err)
		require.Equal(&Default, ec)
	})

	t.Run("default values from empty bytes", func(t *testing.T) {
		require := require.New(t)
		b := []byte(``)
		ec, err := GetConfig(b)
		require.NoError(err)
		require.Equal(&Default, ec)
	})

	t.Run("mix default and extracted values from json", func(t *testing.T) {
		require := require.New(t)
		b := []byte(`{"block-cache-size":1}`)
		ec, err := GetConfig(b)
		require.NoError(err)
		expected := Default
		expected.BlockCacheSize = 1
		require.Equal(&expected, ec)
	})

	t.Run("all values extracted from json", func(t *testing.T) {
		require := require.New(t)

		expected := &Config{
			Network: Network{
				MaxValidatorSetStaleness:                    1,
				TargetGossipSize:                            2,
				PushGossipPercentStake:                      .3,
				PushGossipNumValidators:                     4,
				PushGossipNumPeers:                          5,
				PushRegossipNumValidators:                   6,
				PushRegossipNumPeers:                        7,
				PushGossipDiscardedCacheSize:                8,
				PushGossipMaxRegossipFrequency:              9,
				PushGossipFrequency:                         10,
				PullGossipPollSize:                          11,
				PullGossipFrequency:                         12,
				PullGossipThrottlingPeriod:                  13,
				PullGossipRequestsPerValidator:              14,
				ExpectedBloomFilterElements:                 15,
				ExpectedBloomFilterFalsePositiveProbability: 16,
				MaxBloomFilterFalsePositiveProbability:      17,
			},
			BlockCacheSize:                1,
			TxCacheSize:                   2,
			TransformedSubnetTxCacheSize:  3,
			RewardUTXOsCacheSize:          5,
			ChainCacheSize:                6,
			ChainDBCacheSize:              7,
			BlockIDCacheSize:              8,
			FxOwnerCacheSize:              9,
			SubnetToL1ConversionCacheSize: 10,
			L1WeightsCacheSize:            11,
			L1InactiveValidatorsCacheSize: 12,
			L1SubnetIDNodeIDCacheSize:     13,
			ChecksumsEnabled:              true,
			MempoolPruneFrequency:         time.Minute,
		}
		verifyInitializedStruct(t, *expected)
		verifyInitializedStruct(t, expected.Network)

		b, err := json.Marshal(expected)
		require.NoError(err)

		actual, err := GetConfig(b)
		require.NoError(err)
		require.Equal(expected, actual)
	})
}
