// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/coreth/utils/utilstest"
)

func TestMinDelayExcess(t *testing.T) {
	activatingGraniteConfig := *extras.TestGraniteChainConfig
	activatingGraniteTimestamp := uint64(1000)
	activatingGraniteConfig.NetworkUpgrades.GraniteTimestamp = utils.NewUint64(activatingGraniteTimestamp)

	tests := []struct {
		name                  string
		config                *extras.ChainConfig
		parent                *types.Header
		header                *types.Header
		desiredMinDelayExcess *acp226.DelayExcess
		expectedDelayExcess   *acp226.DelayExcess
		expectedErr           error
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
			desiredMinDelayExcess: nil,
			expectedDelayExcess:   nil,
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
			desiredMinDelayExcess: utilstest.PointerTo(acp226.DelayExcess(1000)),
			expectedDelayExcess:   nil,
		},
		{
			name:   "granite_first_block_initial_delay_excess",
			config: &activatingGraniteConfig,
			parent: &types.Header{
				Time: activatingGraniteTimestamp - 1,
			},
			header: &types.Header{
				Time: activatingGraniteTimestamp + 1,
			},
			desiredMinDelayExcess: nil,
			expectedDelayExcess:   utilstest.PointerTo(acp226.DelayExcess(acp226.InitialDelayExcess)),
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
			desiredMinDelayExcess: nil,
			expectedDelayExcess:   nil,
			expectedErr:           errParentMinDelayExcessNil,
		},
		{
			name:   "granite_with_parent_min_delay",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExcess(1000, 500),
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExcess: nil,
			expectedDelayExcess:   utilstest.PointerTo(acp226.DelayExcess(500)),
		},
		{
			name:   "granite_with_desired_min_delay_excess",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExcess(1000, 500),
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExcess: utilstest.PointerTo(acp226.DelayExcess(1000)),
			expectedDelayExcess:   utilstest.PointerTo(acp226.DelayExcess(500 + acp226.MaxDelayExcessDiff)),
		},
		{
			name:   "granite_with_zero_desired_value",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExcess(1000, 500),
			header: &types.Header{
				Time: 1001,
			},
			desiredMinDelayExcess: utilstest.PointerTo(acp226.DelayExcess(0)),
			expectedDelayExcess:   utilstest.PointerTo(acp226.DelayExcess(500 - acp226.MaxDelayExcessDiff)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := MinDelayExcess(test.config, test.parent, test.header.Time, test.desiredMinDelayExcess)
			require.ErrorIs(t, err, test.expectedErr)
			require.Equal(t, test.expectedDelayExcess, result)
		})
	}
}

func TestVerifyMinDelayExcess(t *testing.T) {
	tests := []struct {
		name        string
		config      *extras.ChainConfig
		parent      *types.Header
		header      *types.Header
		expectedErr error
	}{
		{
			name:   "pre_granite_nil_min_delay_excess",
			config: extras.TestFortunaChainConfig, // Pre-Granite config
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
		},
		{
			name:   "pre_granite_min_delay_excess_set_error",
			config: extras.TestFortunaChainConfig, // Pre-Granite config
			parent: &types.Header{
				Time: 1000,
			},
			header:      generateHeaderWithMinDelayExcess(1001, 1000),
			expectedErr: errRemoteMinDelayExcessSet,
		},
		{
			name:   "granite_nil_min_delay_excess_error",
			config: extras.TestGraniteChainConfig,
			parent: &types.Header{
				Time: 1000,
			},
			header: &types.Header{
				Time: 1001,
			},
			expectedErr: errRemoteMinDelayExcessNil,
		},
		{
			name:        "granite_incorrect_min_delay_excess",
			config:      extras.TestGraniteChainConfig,
			parent:      generateHeaderWithMinDelayExcess(1000, 500),
			header:      generateHeaderWithMinDelayExcess(1001, 1000),
			expectedErr: errIncorrectMinDelayExcess,
		},
		{
			name:        "granite_incorrect_min_delay_excess_with_zero_desired",
			config:      extras.TestGraniteChainConfig,
			parent:      generateHeaderWithMinDelayExcess(1000, 500),
			header:      generateHeaderWithMinDelayExcess(1001, 0),
			expectedErr: errIncorrectMinDelayExcess,
		},
		{
			name:   "granite_correct_min_delay_excess",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExcess(1000, 500),
			header: generateHeaderWithMinDelayExcess(1001, 500),
		},
		{
			name:   "granite_with_increased_desired_min_delay_excess_correct",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExcess(1000, 500),
			header: generateHeaderWithMinDelayExcess(1001, 700),
		},
		{
			name:   "granite_with_decreased_desired_min_delay_excess_correct",
			config: extras.TestGraniteChainConfig,
			parent: generateHeaderWithMinDelayExcess(1000, 500),
			header: generateHeaderWithMinDelayExcess(1001, 300),
		},

		// Different chain configs
		{
			name:   "fortuna_config_no_verification",
			config: extras.TestFortunaChainConfig,
			parent: &types.Header{Time: 1000},
			header: &types.Header{Time: 1001},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyMinDelayExcess(test.config, test.parent, test.header)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}

func generateHeaderWithMinDelayExcess(timeSeconds uint64, minDelayExcess acp226.DelayExcess) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{
			Time: timeSeconds,
		},
		&customtypes.HeaderExtra{
			MinDelayExcess: &minDelayExcess,
		},
	)
}
