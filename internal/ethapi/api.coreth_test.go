// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

type testSuggestPriceOptionsBackend struct {
	Backend // embed the interface to avoid implementing unused methods

	estimateBaseFee  *big.Int
	suggestGasTipCap *big.Int
}

func (b *testSuggestPriceOptionsBackend) EstimateBaseFee(context.Context) (*big.Int, error) {
	return b.estimateBaseFee, nil
}

func (b *testSuggestPriceOptionsBackend) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return b.suggestGasTipCap, nil
}

func TestSuggestPriceOptions(t *testing.T) {
	tests := []struct {
		name             string
		estimateBaseFee  *big.Int
		suggestGasTipCap *big.Int
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
			estimateBaseFee:  bigMinBaseFee,
			suggestGasTipCap: bigMinGasTip,
			want: &PriceOptions{
				Slow: newPrice(
					minGasTip,
					minBaseFee+minGasTip,
				),
				Normal: newPrice(
					minGasTip,
					minBaseFee+minGasTip,
				),
				Fast: newPrice(
					minGasTip,
					(fastFeeNumerator*minBaseFee)/feeDenominator+(fastFeeNumerator*minGasTip)/feeDenominator,
				),
			},
		},
		{
			name:             "maximum_values",
			estimateBaseFee:  bigMaxNormalBaseFee,
			suggestGasTipCap: bigMaxNormalGasTip,
			want: &PriceOptions{
				Slow: newPrice(
					(slowFeeNumerator*maxNormalGasTip)/feeDenominator,
					(slowFeeNumerator*maxNormalBaseFee)/feeDenominator+(slowFeeNumerator*maxNormalGasTip)/feeDenominator,
				),
				Normal: newPrice(
					maxNormalGasTip,
					maxNormalBaseFee+maxNormalGasTip,
				),
				Fast: newPrice(
					(fastFeeNumerator*maxNormalGasTip)/feeDenominator,
					(fastFeeNumerator*maxNormalBaseFee)/feeDenominator+(fastFeeNumerator*maxNormalGasTip)/feeDenominator,
				),
			},
		},
		{
			name:             "double_maximum_values",
			estimateBaseFee:  big.NewInt(2 * maxNormalBaseFee),
			suggestGasTipCap: big.NewInt(2 * maxNormalGasTip),
			want: &PriceOptions{
				Slow: newPrice(
					(slowFeeNumerator*maxNormalGasTip)/feeDenominator,
					(slowFeeNumerator*maxNormalBaseFee)/feeDenominator+(slowFeeNumerator*maxNormalGasTip)/feeDenominator,
				),
				Normal: newPrice(
					maxNormalGasTip,
					maxNormalBaseFee+maxNormalGasTip,
				),
				Fast: newPrice(
					(fastFeeNumerator*2*maxNormalGasTip)/feeDenominator,
					(fastFeeNumerator*2*maxNormalBaseFee)/feeDenominator+(fastFeeNumerator*2*maxNormalGasTip)/feeDenominator,
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
			}
			api := NewEthereumAPI(backend)

			got, err := api.SuggestPriceOptions(context.Background())
			require.NoError(err)
			require.Equal(test.want, got)
		})
	}
}

func newPrice(gasTip, gasFee int64) *Price {
	return &Price{
		GasTip: (*hexutil.Big)(big.NewInt(gasTip)),
		GasFee: (*hexutil.Big)(big.NewInt(gasFee)),
	}
}
