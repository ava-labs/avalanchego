// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/vms/evm/acp176"
)

type testSuggestPriceOptionsBackend struct {
	Backend // embed the interface to avoid implementing unused methods

	estimateBaseFee  *big.Int
	suggestGasTipCap *big.Int

	cfg PriceOptionConfig
}

func (b *testSuggestPriceOptionsBackend) EstimateBaseFee(context.Context) (*big.Int, error) {
	return b.estimateBaseFee, nil
}

func (b *testSuggestPriceOptionsBackend) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return b.suggestGasTipCap, nil
}

func (b *testSuggestPriceOptionsBackend) PriceOptionsConfig() PriceOptionConfig {
	return b.cfg
}

func TestSuggestPriceOptions(t *testing.T) {
	testCfg := PriceOptionConfig{
		SlowFeePercentage: 95,
		FastFeePercentage: 105,
		MaxTip:            20 * params.GWei,
	}
	slowFeeNumerator := testCfg.SlowFeePercentage
	fastFeeNumerator := testCfg.FastFeePercentage
	maxNormalGasTip := testCfg.MaxTip

	tests := []struct {
		name             string
		estimateBaseFee  *big.Int
		suggestGasTipCap *big.Int
		cfg              PriceOptionConfig
		want             *PriceOptions
	}{
		{
			name:             "nil_base_fee",
			estimateBaseFee:  nil,
			suggestGasTipCap: common.Big1,
			want:             nil,
		},
		{
			name:             "nil_tip_cap",
			estimateBaseFee:  common.Big1,
			suggestGasTipCap: nil,
			want:             nil,
		},
		{
			name:             "minimum_values",
			estimateBaseFee:  big.NewInt(acp176.MinGasPrice),
			suggestGasTipCap: bigMinGasTip,
			cfg:              testCfg,
			want: &PriceOptions{
				Slow: newPrice(
					minGasTip,
					2*acp176.MinGasPrice+minGasTip,
				),
				Normal: newPrice(
					minGasTip,
					2*acp176.MinGasPrice+minGasTip,
				),
				Fast: newPrice(
					minGasTip,
					2*acp176.MinGasPrice+(fastFeeNumerator*minGasTip/feeDenominator),
				),
			},
		},
		{
			name:             "maximum_values_1_slow_2_fast",
			estimateBaseFee:  big.NewInt(100 * params.GWei),
			suggestGasTipCap: new(big.Int).SetUint64(maxNormalGasTip),
			cfg: PriceOptionConfig{
				SlowFeePercentage: 100,
				FastFeePercentage: 200,
				MaxTip:            20 * params.GWei,
			},
			want: &PriceOptions{
				Slow: newPrice(
					20*params.GWei,
					220*params.GWei,
				),
				Normal: newPrice(
					20*params.GWei,
					220*params.GWei,
				),
				Fast: newPrice(
					40*params.GWei,
					240*params.GWei,
				),
			},
		},
		{
			name:             "maximum_value",
			cfg:              testCfg,
			estimateBaseFee:  big.NewInt(100 * params.GWei),
			suggestGasTipCap: new(big.Int).SetUint64(maxNormalGasTip),
			want: &PriceOptions{
				Slow: newPrice(
					(slowFeeNumerator*maxNormalGasTip)/feeDenominator,
					2*100*params.GWei+(slowFeeNumerator*maxNormalGasTip)/feeDenominator,
				),
				Normal: newPrice(
					maxNormalGasTip,
					2*100*params.GWei+maxNormalGasTip,
				),
				Fast: newPrice(
					(fastFeeNumerator*maxNormalGasTip)/feeDenominator,
					2*100*params.GWei+(fastFeeNumerator*maxNormalGasTip)/feeDenominator,
				),
			},
		},
		{
			name:             "double_maximum_values",
			estimateBaseFee:  big.NewInt(100 * params.GWei),
			suggestGasTipCap: big.NewInt(2 * int64(maxNormalGasTip)),
			cfg:              testCfg,
			want: &PriceOptions{
				Slow: newPrice(
					(slowFeeNumerator*maxNormalGasTip)/feeDenominator,
					2*100*params.GWei+(slowFeeNumerator*maxNormalGasTip)/feeDenominator,
				),
				Normal: newPrice(
					maxNormalGasTip,
					2*100*params.GWei+maxNormalGasTip,
				),
				Fast: newPrice(
					(fastFeeNumerator*2*maxNormalGasTip)/feeDenominator,
					2*100*params.GWei+(fastFeeNumerator*2*maxNormalGasTip)/feeDenominator,
				),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			backend := &testSuggestPriceOptionsBackend{
				estimateBaseFee:  test.estimateBaseFee,
				suggestGasTipCap: test.suggestGasTipCap,
				cfg:              test.cfg,
			}
			api := NewEthereumAPI(backend)

			got, err := api.SuggestPriceOptions(t.Context())
			require.NoError(err)
			require.Equal(test.want, got)
		})
	}
}

func newPrice(gasTip, gasFee uint64) *Price {
	return &Price{
		GasTip: (*hexutil.Big)(new(big.Int).SetUint64(gasTip)),
		GasFee: (*hexutil.Big)(new(big.Int).SetUint64(gasFee)),
	}
}
