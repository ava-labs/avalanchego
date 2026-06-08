// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"math"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params/extras"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

func clampedToward(parent, target dynamic.PriceExponent) dynamic.PriceExponent {
	return parent.Toward(&target)
}

func TestMinPriceExponent(t *testing.T) {
	activatingHeliconConfig := *extras.TestHeliconChainConfig
	const activationTime = uint64(1000)
	activatingHeliconConfig.NetworkUpgrades.HeliconTimestamp = utils.PointerTo(activationTime)

	parentExponent := dynamic.PriceExponent(1000)

	tests := []struct {
		name      string
		config    *extras.ChainConfig
		parent    *types.Header
		timestamp uint64
		desired   *dynamic.PriceExponent
		want      *dynamic.PriceExponent
	}{
		{
			name:      "pre_helicon_ignores_desired",
			config:    extras.TestGraniteChainConfig,
			parent:    &types.Header{Time: 100},
			timestamp: 101,
			desired:   utils.PointerTo(dynamic.PriceExponent(1000)),
		},
		{
			name:      "activation_seeds_initial",
			config:    &activatingHeliconConfig,
			parent:    &types.Header{Time: activationTime - 1},
			timestamp: activationTime + 1,
			want:      utils.PointerTo(dynamic.InitialPriceExponent),
		},
		{
			name:      "carry_parent",
			config:    extras.TestHeliconChainConfig,
			parent:    headerWithMinPriceExponent(1000, parentExponent),
			timestamp: 1001,
			want:      utils.PointerTo(parentExponent),
		},
		{
			name:      "desired_within_cap",
			config:    extras.TestHeliconChainConfig,
			parent:    headerWithMinPriceExponent(1000, parentExponent),
			timestamp: 1001,
			desired:   utils.PointerTo(parentExponent + 200),
			want:      utils.PointerTo(parentExponent + 200),
		},
		{
			name:      "desired_clamped_up",
			config:    extras.TestHeliconChainConfig,
			parent:    headerWithMinPriceExponent(1000, parentExponent),
			timestamp: 1001,
			desired:   utils.PointerTo(dynamic.PriceExponent(math.MaxUint64)),
			want:      utils.PointerTo(clampedToward(parentExponent, math.MaxUint64)),
		},
		{
			name:      "desired_clamped_down",
			config:    extras.TestHeliconChainConfig,
			parent:    headerWithMinPriceExponent(1000, parentExponent),
			timestamp: 1001,
			desired:   utils.PointerTo(dynamic.PriceExponent(0)),
			want:      utils.PointerTo(clampedToward(parentExponent, 0)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := MinPriceExponent(test.config, test.parent, test.timestamp, test.desired)
			require.Equal(t, test.want, got)
		})
	}
}

func TestVerifyMinPriceExponent(t *testing.T) {
	parentExponent := dynamic.PriceExponent(1000)

	tests := []struct {
		name    string
		config  *extras.ChainConfig
		parent  *types.Header
		header  *types.Header
		wantErr error
	}{
		{
			name:   "pre_helicon_skips",
			config: extras.TestGraniteChainConfig,
			parent: &types.Header{Time: 1000},
			header: &types.Header{Time: 1001},
		},
		{
			name:    "pre_helicon_rejects_set_value",
			config:  extras.TestGraniteChainConfig,
			parent:  &types.Header{Time: 1000},
			header:  headerWithMinPriceExponent(1001, parentExponent),
			wantErr: errRemoteMinPriceExponentSet,
		},
		{
			name:    "missing_value",
			config:  extras.TestHeliconChainConfig,
			parent:  &types.Header{Time: 1000},
			header:  &types.Header{Time: 1001},
			wantErr: errRemoteMinPriceExponentNil,
		},
		{
			name:    "claim_exceeds_clamp_up",
			config:  extras.TestHeliconChainConfig,
			parent:  headerWithMinPriceExponent(1000, parentExponent),
			header:  headerWithMinPriceExponent(1001, math.MaxUint64-1),
			wantErr: errIncorrectMinPriceExponent,
		},
		{
			name:    "claim_exceeds_clamp_down",
			config:  extras.TestHeliconChainConfig,
			parent:  headerWithMinPriceExponent(1000, math.MaxUint64-1),
			header:  headerWithMinPriceExponent(1001, 0),
			wantErr: errIncorrectMinPriceExponent,
		},
		{
			name:   "unchanged",
			config: extras.TestHeliconChainConfig,
			parent: headerWithMinPriceExponent(1000, parentExponent),
			header: headerWithMinPriceExponent(1001, parentExponent),
		},
		{
			name:   "within_cap",
			config: extras.TestHeliconChainConfig,
			parent: headerWithMinPriceExponent(1000, parentExponent),
			header: headerWithMinPriceExponent(1001, parentExponent+200),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := VerifyMinPriceExponent(test.config, test.parent, test.header)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func headerWithMinPriceExponent(time uint64, exponent dynamic.PriceExponent) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{Time: time},
		&customtypes.HeaderExtra{MinPriceExponent: &exponent},
	)
}
