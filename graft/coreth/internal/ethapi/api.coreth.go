// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/common/math"
)

const (
	minGasTip      = 1 // 1 wei
	feeDenominator = 100
)

var (
	bigMinGasTip      = big.NewInt(minGasTip)
	bigFeeDenominator = big.NewInt(feeDenominator)
)

type PriceOptionConfig struct {
	SlowFeePercentage uint64
	FastFeePercentage uint64
	MaxTip            uint64
}

type Price struct {
	GasTip *hexutil.Big `json:"maxPriorityFeePerGas"`
	GasFee *hexutil.Big `json:"maxFeePerGas"`
}

type PriceOptions struct {
	Slow   *Price `json:"slow"`
	Normal *Price `json:"normal"`
	Fast   *Price `json:"fast"`
}

// TODO: This can be moved to AVAX/custom API

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

	cfg := s.b.PriceOptionsConfig()
	gasTips := calculateFeeSpeeds(
		bigMinGasTip,
		gasTip,
		new(big.Int).SetUint64(cfg.MaxTip),
		new(big.Int).SetUint64(cfg.SlowFeePercentage),
		new(big.Int).SetUint64(cfg.FastFeePercentage),
	)

	// Double the baseFee estimate without modifying the original variable.
	baseFeeDouble := new(big.Int).Lsh(baseFee, 1)

	slowGasFee := new(big.Int).Add(baseFeeDouble, gasTips.slow)
	normalGasFee := new(big.Int).Add(baseFeeDouble, gasTips.normal)
	fastGasFee := new(big.Int).Add(baseFeeDouble, gasTips.fast)
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
// slow   = max(slowFeePerc/100 * min(estimate, maxFee), minFee)
// normal = min(estimate, maxFee)
// fast   = fastFeePerc/100 * estimate
func calculateFeeSpeeds(
	minFee *big.Int,
	estimate *big.Int,
	maxFee *big.Int,
	slowFeePerc *big.Int,
	fastFeePerc *big.Int,
) feeSpeeds {
	// Cap the fee to keep slow and normal options reasonable during fee spikes.
	cappedFee := math.BigMin(estimate, maxFee)

	slowFee := new(big.Int).Set(cappedFee)
	slowFee.Mul(slowFee, slowFeePerc)
	slowFee.Div(slowFee, bigFeeDenominator)
	slowFee = math.BigMax(slowFee, minFee)

	normalFee := cappedFee

	fastFee := new(big.Int).Set(estimate)
	fastFee.Mul(fastFee, fastFeePerc)
	fastFee.Div(fastFee, bigFeeDenominator)
	return feeSpeeds{
		slow:   slowFee,
		normal: normalFee,
		fast:   fastFee,
	}
}
