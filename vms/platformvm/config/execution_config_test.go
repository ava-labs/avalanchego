// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/platformvm/network"
)

// Errors if all values in a struct are not initialized
func VerifyInitializedStruct(s interface{}) error {
	structType := reflect.TypeOf(s)
	if structType.Kind() != reflect.Struct {
		return fmt.Errorf("expected struct, got %s", structType.Kind())
	}

	v := reflect.ValueOf(s)
	log := ""
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		isSet := field.IsValid() && !field.IsZero()
		if !isSet {
			log = fmt.Sprintf("%v%s not set; ", log, structType.Field(i).Name)
		}

	}

	if len(log) > 0 {
		return errors.New(log)
	}

	return nil
}

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

		expected := &ExecutionConfig{
			Network: network.Config{
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
				PullGossipThrottlingLimit:                   14,
				ExpectedBloomFilterElements:                 15,
				ExpectedBloomFilterFalsePositiveProbability: 16,
				MaxBloomFilterFalsePositiveProbability:      17,
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
		require.NoError(VerifyInitializedStruct(*expected))
		require.NoError(VerifyInitializedStruct(expected.Network))

		b, err := json.Marshal(expected)
		require.NoError(err)

		actual, err := GetExecutionConfig(b)
		require.NoError(err)
		require.Equal(expected, actual)
	})
}
