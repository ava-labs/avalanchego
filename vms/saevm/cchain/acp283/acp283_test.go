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
		name     string
		exponent PriceExponent
		price    gas.Price
	}{
		{
			name:     "zero",
			exponent: 0,
			price:    minPriceWei,
		},
		{
			name:     "min_exponent_change",
			exponent: 288_230_376_151_711_749, // smallest exponent that raises the price above minPriceWei
			price:    minPriceWei + 1,
		},
		{
			name:     "1_navax",
			exponent: 8_617_325_259_044_912_670,
			price:    1_000_000_000,
		},
		{
			name:     "100_navax",
			exponent: 10_532_286_427_721_559_929,
			price:    100_000_000_000,
		},
		{
			name:     "max_price",
			exponent: maxExponent, // smallest exponent at which the price saturates
			price:    math.MaxUint64,
		},
		{
			name:     "max_exponent",
			exponent: math.MaxUint64,
			price:    math.MaxUint64,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.price, test.exponent.Price())
		})
	}
}

func TestToward(t *testing.T) {
	tests := []struct {
		name     string
		initial  PriceExponent
		desired  PriceExponent
		expected PriceExponent
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
			desired:  10 * maxPriceExponentDiff,
			expected: maxPriceExponentDiff,
		},
		{
			name:     "decrease_clamped",
			initial:  10 * maxPriceExponentDiff,
			desired:  0,
			expected: 9 * maxPriceExponentDiff,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			exponent := test.initial
			exponent.Toward(test.desired)
			require.Equal(t, test.expected, exponent)
		})
	}
}

func TestDesiredPriceExponent(t *testing.T) {
	tests := []struct {
		name     string
		price    gas.Price
		exponent PriceExponent
	}{
		{
			name:     "zero",
			price:    minPriceWei,
			exponent: 0,
		},
		{
			name:     "min_exponent_change",
			price:    minPriceWei + 1,
			exponent: 288_230_376_151_711_749,
		},
		{
			name:     "1_nAvax",
			price:    1_000_000_000,
			exponent: 8_617_325_259_044_912_670,
		},
		{
			name:     "100_nAvax",
			price:    100_000_000_000,
			exponent: 10_532_286_427_721_559_929,
		},
		{
			name:     "max_price",
			price:    math.MaxUint64,
			exponent: maxExponent,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.exponent, DesiredPriceExponent(test.price))
		})
	}
}
