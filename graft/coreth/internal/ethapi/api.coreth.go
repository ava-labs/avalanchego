// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/coreth/plugin/evm/upgrade/etna"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
)

const (
	nAVAX = 1_000_000_000

	minBaseFee       = etna.MinBaseFee // 1 nAVAX
	maxNormalBaseFee = 100 * nAVAX

	minGasTip       = 1 // 1 wei
	maxNormalGasTip = 20 * nAVAX

	slowFeeNumerator = 19 // 19/20 = 0.95
	fastFeeNumerator = 21 // 21/20 = 1.05
	feeDenominator   = 20
)

var (
	bigMinBaseFee       = big.NewInt(minBaseFee)
	bigMaxNormalBaseFee = big.NewInt(maxNormalBaseFee)

	bigMinGasTip       = big.NewInt(minGasTip)
	bigMaxNormalGasTip = big.NewInt(maxNormalGasTip)

	bigSlowFeeNumerator = big.NewInt(slowFeeNumerator)
	bigFastFeeNumerator = big.NewInt(fastFeeNumerator)
	bigFeeDenominator   = big.NewInt(feeDenominator)
)

type Price struct {
	GasTip *hexutil.Big `json:"maxPriorityFeePerGas"`
	GasFee *hexutil.Big `json:"maxFeePerGas"`
}

type PriceOptions struct {
	Slow   *Price `json:"slow"`
	Normal *Price `json:"normal"`
	Fast   *Price `json:"fast"`
}

// SuggestPriceOptions returns suggestions for what to display to a user for
// current transaction fees.
func (s *EthereumAPI) SuggestPriceOptions(ctx context.Context) (*PriceOptions, error) {
	baseFee, err := s.b.EstimateBaseFee(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to estimate base fee: %w", err)
	}
	gasTip, err := s.b.SuggestGasTipCap(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to suggest gas tip cap: %w", err)
	}

	// If the chain isn't running with dynamic fees, return nil.
	if baseFee == nil || gasTip == nil {
		return nil, nil
	}

	baseFees := calculateFeeSpeeds(
		bigMinBaseFee,
		baseFee,
		bigMaxNormalBaseFee,
	)
	gasTips := calculateFeeSpeeds(
		bigMinGasTip,
		gasTip,
		bigMaxNormalGasTip,
	)
	slowGasFee := new(big.Int).Add(baseFees.slow, gasTips.slow)
	normalGasFee := new(big.Int).Add(baseFees.normal, gasTips.normal)
	fastGasFee := new(big.Int).Add(baseFees.fast, gasTips.fast)
	return &PriceOptions{
		Slow: &Price{
			GasTip: (*hexutil.Big)(gasTips.slow),
			GasFee: (*hexutil.Big)(slowGasFee),
		},
		Normal: &Price{
			GasTip: (*hexutil.Big)(gasTips.normal),
			GasFee: (*hexutil.Big)(normalGasFee),
		},
		Fast: &Price{
			GasTip: (*hexutil.Big)(gasTips.fast),
			GasFee: (*hexutil.Big)(fastGasFee),
		},
	}, nil
}

type feeSpeeds struct {
	slow   *big.Int
	normal *big.Int
	fast   *big.Int
}

// calculateFeeSpeeds returns the slow, normal, and fast price options for a
// given min, estimate, and max,
//
// slow   = max(0.95 * min(estimate, maxFee), minFee)
// normal = min(estimate, maxFee)
// fast   = 1.05 * estimate
func calculateFeeSpeeds(
	minFee *big.Int,
	estimate *big.Int,
	maxFee *big.Int,
) feeSpeeds {
	// Cap the fee to keep slow and normal options reasonable during fee spikes.
	cappedFee := math.BigMin(estimate, maxFee)

	slowFee := new(big.Int).Set(cappedFee)
	slowFee.Mul(slowFee, bigSlowFeeNumerator)
	slowFee.Div(slowFee, bigFeeDenominator)
	slowFee = math.BigMax(slowFee, minFee)

	normalFee := cappedFee

	fastFee := new(big.Int).Set(estimate)
	fastFee.Mul(fastFee, bigFastFeeNumerator)
	fastFee.Div(fastFee, bigFeeDenominator)
	return feeSpeeds{
		slow:   slowFee,
		normal: normalFee,
		fast:   fastFee,
	}
}
