// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/avm/network"
)

func TestParseConfig(t *testing.T) {
	tests := []struct {
		name           string
		configBytes    []byte
		expectedConfig Config
	}{
		{
			name:           "unspecified config",
			configBytes:    nil,
			expectedConfig: DefaultConfig,
		},
		{
			name:        "manually specified checksums enabled",
			configBytes: []byte(`{"checksums-enabled":true}`),
			expectedConfig: Config{
				Network:          network.DefaultConfig,
				ChecksumsEnabled: true,
			},
		},
		{
			name:        "manually specified network value",
			configBytes: []byte(`{"network":{"max-validator-set-staleness":1}}`),
			expectedConfig: Config{
				Network: network.Config{
					MaxValidatorSetStaleness:                    time.Nanosecond,
					TargetGossipSize:                            network.DefaultConfig.TargetGossipSize,
					PushGossipPercentStake:                      network.DefaultConfig.PushGossipPercentStake,
					PushGossipNumValidators:                     network.DefaultConfig.PushGossipNumValidators,
					PushGossipNumPeers:                          network.DefaultConfig.PushGossipNumPeers,
					PushRegossipNumValidators:                   network.DefaultConfig.PushRegossipNumValidators,
					PushRegossipNumPeers:                        network.DefaultConfig.PushRegossipNumPeers,
					PushGossipDiscardedCacheSize:                network.DefaultConfig.PushGossipDiscardedCacheSize,
					PushGossipMaxRegossipFrequency:              network.DefaultConfig.PushGossipMaxRegossipFrequency,
					PushGossipFrequency:                         network.DefaultConfig.PushGossipFrequency,
					PullGossipFrequency:                         network.DefaultConfig.PullGossipFrequency,
					PullGossipThrottlingPeriod:                  network.DefaultConfig.PullGossipThrottlingPeriod,
					PullGossipRequestsPerValidator:              network.DefaultConfig.PullGossipRequestsPerValidator,
					ExpectedBloomFilterElements:                 network.DefaultConfig.ExpectedBloomFilterElements,
					ExpectedBloomFilterFalsePositiveProbability: network.DefaultConfig.ExpectedBloomFilterFalsePositiveProbability,
					MaxBloomFilterFalsePositiveProbability:      network.DefaultConfig.MaxBloomFilterFalsePositiveProbability,
				},
				ChecksumsEnabled: DefaultConfig.ChecksumsEnabled,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config, err := ParseConfig(test.configBytes)
			require.NoError(err)
			require.Equal(test.expectedConfig, config)
		})
	}
}
