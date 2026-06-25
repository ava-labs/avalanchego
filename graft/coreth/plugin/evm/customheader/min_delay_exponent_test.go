// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

func TestMinDelayExponent(t *testing.T) {
	activatingGraniteConfig := *extras.TestGraniteChainConfig
	activatingGraniteTimestamp := uint64(1000)
	activatingGraniteConfig.NetworkUpgrades.GraniteTimestamp = utils.PointerTo(activatingGraniteTimestamp)

	tests := []struct {
		name                    string
		config                  *extras.ChainConfig
		parent                  *types.Header
		header                  *types.Header
		desiredMinDelayExponent *dynamic.DelayExponent
		expectedDelayExponent   *dynamic.DelayExponent
		expectedErr             error
	}{
		// Pre-Granite tests
		{
			name:   "pre_granite_returns_nil",
			config: extras.TestFortunaChainConfig, // Pre-Granite config
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExponent: nil,
			expectedDelayExponent:   nil,
		},
		{
			name:   "pre_granite_with_desired_value_returns_nil",
			config: extras.TestFortunaChainConfig, // Pre-Granite config
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExponent: utils.PointerTo(dynamic.DelayExponent(1000)),
			expectedDelayExponent:   nil,
		},
		{
			name:   "granite_first_block_initial_delay_exponent",
			config: &activatingGraniteConfig,
			parent: &types.Header{
				Time: activatingGraniteTimestamp - 1,
			},
			header: &types.Header{
				Time: activatingGraniteTimestamp + 1,
			},
			desiredMinDelayExponent: nil,
			expectedDelayExponent:   utils.PointerTo(dynamic.InitialDelayExponent),
		},
		{
			name:   "granite_no_parent_min_delay_error",
			config: extras.TestGraniteChainConfig,
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExponent: nil,
			expectedDelayExponent:   nil,
			expectedErr:             errParentMinDelayExponentNil,
		},
		{
			name:   "granite_with_parent_min_delay",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExponent(1000, 500),
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExponent: nil,
			expectedDelayExponent:   utils.PointerTo(dynamic.DelayExponent(500)),
		},
		{
			name:   "granite_with_desired_min_delay_exponent",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExponent(1000, 500),
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExponent: utils.PointerTo(dynamic.DelayExponent(1000)),
			expectedDelayExponent:   utils.PointerTo(dynamic.DelayExponent(500 + 200)),
		},
		{
			name:   "granite_with_zero_desired_value",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExponent(1000, 500),
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExponent: utils.PointerTo(dynamic.DelayExponent(0)),
			expectedDelayExponent:   utils.PointerTo(dynamic.DelayExponent(500 - 200)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := MinDelayExponent(test.config, test.parent, test.header.Time, test.desiredMinDelayExponent)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expectedDelayExponent, result)
		})
	}
}

func TestVerifyMinDelayExponent(t *testing.T) {
	tests := []struct {
		name        string
		config      *extras.ChainConfig
		parent      *types.Header
		header      *types.Header
		expectedErr error
	}{
		{
			name:   "pre_granite_nil_min_delay_exponent_success",
			config: extras.TestFortunaChainConfig,
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
		},
		{
			name:   "nil_min_delay_exponent_error",
			config: extras.TestGraniteChainConfig,
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			expectedErr: errRemoteMinDelayExponentNil,
		},
		{
			name:        "incorrect_min_delay_exponent",
			config:      extras.TestGraniteChainConfig,
			parent:      generateHeaderWithMinDelayExponent(1000, 500),
			header:      generateHeaderWithMinDelayExponent(1001, 1000),
			expectedErr: errIncorrectMinDelayExponent,
		},
		{
			name:        "incorrect_min_delay_exponent_with_zero_desired",
			config:      extras.TestGraniteChainConfig,
			parent:      generateHeaderWithMinDelayExponent(1000, 500),
			header:      generateHeaderWithMinDelayExponent(1001, 0),
			expectedErr: errIncorrectMinDelayExponent,
		},
		{
			name:   "correct_min_delay_exponent",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExponent(1000, 500),
			header: generateHeaderWithMinDelayExponent(1001, 500),
		},
		{
			name:   "increased_desired_min_delay_exponent_correct",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExponent(1000, 500),
			header: generateHeaderWithMinDelayExponent(1001, 700),
		},
		{
			name:   "decreased_desired_min_delay_exponent_correct",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExponent(1000, 500),
			header: generateHeaderWithMinDelayExponent(1001, 300),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyMinDelayExponent(test.config, test.parent, test.header)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

func generateHeaderWithMinDelayExponent(timeSeconds uint64, minDelayExponent dynamic.DelayExponent) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{
			Time: timeSeconds,
		},
		&customtypes.HeaderExtra{
			MinDelayExponent: &minDelayExponent,
		},
	)
}
