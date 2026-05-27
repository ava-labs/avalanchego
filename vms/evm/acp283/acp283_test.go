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
			price:  MinPriceWei,
		},
		{
			name:   "min_excess_change",
			excess: 726_820, // smallest excess that increases the price above MinPriceWei
			price:  MinPriceWei + 1,
		},
		{
			name:   "max_initial_excess_change",
			excess: MaxPriceExcessDiff,
			price:  MinPriceWei,
		},
		{
			name:   "1M_wei",
			excess: 14_486_613, // ConversionRate (2^20) * ln(10^6)
			price:  1_000_000,
		},
		{
			name:   "initial",
			excess: InitialPriceExcess,
			price:  10_000_006,
		},
		{
			name:   "qmax_binding",
			excess: 26_558_791, // smallest excess at which the QMaxWei cap binds
			price:  QMaxWei,
		},
		{
			name:   "max_excess",
			excess: maxPriceExcess,
			price:  QMaxWei,
		},
		{
			name:   "largest_excess",
			excess: math.MaxUint64,
			price:  QMaxWei,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.price, test.excess.Price())
		})
	}
}

func TestUpdatePriceExcess(t *testing.T) {
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
			name:     "within_range_up",
			initial:  50,
			desired:  150,
			expected: 150,
		},
		{
			name:     "within_range_down",
			initial:  150,
			desired:  50,
			expected: 50,
		},
		{
			name:     "clamped_up",
			initial:  0,
			desired:  1000,
			expected: MaxPriceExcessDiff,
		},
		{
			name:     "clamped_down",
			initial:  1000,
			desired:  0,
			expected: 1000 - MaxPriceExcessDiff,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.initial.UpdatePriceExcess(test.desired)
			require.Equal(t, test.expected, test.initial)
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
			price:  MinPriceWei,
			excess: 0,
		},
		{
			name:   "min_excess_change",
			price:  MinPriceWei + 1,
			excess: 726_820,
		},
		{
			name:   "1M_wei",
			price:  1_000_000,
			excess: 14_486_613,
		},
		{
			name:   "initial",
			price:  10_000_006,
			excess: InitialPriceExcess,
		},
		{
			name:   "qmax_binding",
			price:  QMaxWei,
			excess: 26_558_791,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.excess, DesiredPriceExcess(test.price))
		})
	}
}
