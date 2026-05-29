// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp283

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

func TestPrice(t *testing.T) {
	tests := []struct {
		name   string
		excess PriceExcess
		price  gas.Price
	}{
		{
			name:   "zero",
			excess: 0,
			price:  minPriceWei,
		},
		{
			name:   "min_excess_change",
			excess: 288_230_376_151_711_749, // smallest excess that raises the price above minPriceWei
			price:  minPriceWei + 1,
		},
		{
			name:   "1_navax",
			excess: 8_617_325_259_044_912_670,
			price:  1_000_000_000,
		},
		{
			name:   "100_navax",
			excess: 10_532_286_427_721_559_929,
			price:  100_000_000_000,
		},
		{
			name:   "max_price",
			excess: maxExponent, // smallest excess at which the price saturates
			price:  math.MaxUint64,
		},
		{
			name:   "max_excess",
			excess: math.MaxUint64,
			price:  math.MaxUint64,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.price, test.excess.Price())
		})
	}
}

func TestToward(t *testing.T) {
	tests := []struct {
		name     string
		initial  PriceExcess
		desired  PriceExcess
		expected PriceExcess
	}{
		{
			name:     "no_change",
			initial:  42,
			desired:  42,
			expected: 42,
		},
		{
			name:     "increase_within_cap",
			initial:  50,
			desired:  100,
			expected: 100,
		},
		{
			name:     "decrease_within_cap",
			initial:  100,
			desired:  50,
			expected: 50,
		},
		{
			name:     "increase_clamped",
			initial:  0,
			desired:  10 * maxPriceExcessDiff,
			expected: maxPriceExcessDiff,
		},
		{
			name:     "decrease_clamped",
			initial:  10 * maxPriceExcessDiff,
			desired:  0,
			expected: 9 * maxPriceExcessDiff,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			excess := test.initial
			excess.Toward(test.desired)
			require.Equal(t, test.expected, excess)
		})
	}
}

func TestDesiredPriceExcess(t *testing.T) {
	tests := []struct {
		name   string
		price  gas.Price
		excess PriceExcess
	}{
		{
			name:   "zero",
			price:  minPriceWei,
			excess: 0,
		},
		{
			name:   "min_excess_change",
			price:  minPriceWei + 1,
			excess: 288_230_376_151_711_749,
		},
		{
			name:   "1_nAvax",
			price:  1_000_000_000,
			excess: 8_617_325_259_044_912_670,
		},
		{
			name:   "100_nAvax",
			price:  100_000_000_000,
			excess: 10_532_286_427_721_559_929,
		},
		{
			name:   "max_price",
			price:  math.MaxUint64,
			excess: maxExponent,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.excess, DesiredPriceExcess(test.price))
		})
	}
}
