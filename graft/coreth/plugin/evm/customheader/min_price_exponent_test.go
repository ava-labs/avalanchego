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
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/acp283"
)

func clampedToward(parent, target acp283.PriceExponent) acp283.PriceExponent {
	parent.Toward(target)
	return parent
}

func TestMinPriceExponent(t *testing.T) {
	activatingHeliconConfig := *extras.TestHeliconChainConfig
	const activationTime = uint64(1000)
	activatingHeliconConfig.NetworkUpgrades.HeliconTimestamp = utils.PointerTo(activationTime)

	parentExponent := acp283.PriceExponent(1000)

	tests := []struct {
		name      string
		config    *extras.ChainConfig
		parent    *types.Header
		timestamp uint64
		desired   *acp283.PriceExponent
		want      *acp283.PriceExponent
		wantErr   error
	}{
		{
			name:      "pre_helicon_ignores_desired",
			config:    extras.TestGraniteChainConfig,
			parent:    &types.Header{Time: 100},
			timestamp: 101,
			desired:   utils.PointerTo(acp283.PriceExponent(1000)),
		},
		{
			name:      "activation_seeds_initial",
			config:    &activatingHeliconConfig,
			parent:    &types.Header{Time: activationTime - 1},
			timestamp: activationTime + 1,
			want:      utils.PointerTo(acp283.InitialPriceExponent),
		},
		{
			name:      "missing_parent_value",
			config:    extras.TestHeliconChainConfig,
			parent:    &types.Header{Time: 1000},
			timestamp: 1001,
			wantErr:   errParentMinPriceExponentNil,
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
			desired:   utils.PointerTo(acp283.PriceExponent(math.MaxUint64)),
			want:      utils.PointerTo(clampedToward(parentExponent, math.MaxUint64)),
		},
		{
			name:      "desired_clamped_down",
			config:    extras.TestHeliconChainConfig,
			parent:    headerWithMinPriceExponent(1000, parentExponent),
			timestamp: 1001,
			desired:   utils.PointerTo(acp283.PriceExponent(0)),
			want:      utils.PointerTo(clampedToward(parentExponent, 0)),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := MinPriceExponent(test.config, test.parent, test.timestamp, test.desired)
			require.ErrorIs(t, err, test.wantErr)
			require.Equal(t, test.want, got)
		})
	}
}

func TestVerifyMinPriceExponent(t *testing.T) {
	parentExponent := acp283.PriceExponent(1000)

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

func headerWithMinPriceExponent(time uint64, exponent acp283.PriceExponent) *types.Header {
	return customtypes.WithHeaderExtra(
		&types.Header{Time: time},
		&customtypes.HeaderExtra{MinPriceExponent: &exponent},
	)
}
