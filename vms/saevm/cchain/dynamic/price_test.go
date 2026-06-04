// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// Independent copies of the implementation's constants. Sharing them would make
// the assertions tautological.
const (
	maxPriceDiff     = 288_230_376_151_711_744 / 3_600    // PriceExponent.Toward step cap
	maxPriceExponent = PriceExponent(math.MaxUint64 - 37) // saturates Price to MaxUint64
)

// priceReaderCases pin each price to its smallest yielding exponent. ACP-283 - exponents are computed. The prices show the doubling
// intent: 1, 2, 4, 8.
var priceReaderCases = []readerCase[PriceExponent, gas.Price]{
	{name: "minimum", exponent: 0, value: 1},
	{name: "shared_minimum", exponent: 1, value: 1, skipDesired: true},
	{name: "double", exponent: 288_230_376_151_711_749, value: 2},            // conversionRate * ln(2)
	{name: "quadruple", exponent: 576_460_752_303_423_490, value: 4},         // conversionRate * ln(4)
	{name: "octuple", exponent: 864_691_128_455_135_233, value: 8},           // conversionRate * ln(8)
	{name: "hundred", exponent: 1_914_961_168_676_647_261, value: 100},       // conversionRate * ln(100)
	{name: "million", exponent: 5_744_883_506_029_941_780, value: 1_000_000}, // conversionRate * ln(1_000_000)
	{name: "2^40", exponent: 11_529_215_046_068_469_736, value: 1 << 40},     // conversionRate * ln(2^40)
	{name: "largest_uint64", exponent: maxPriceExponent, value: math.MaxUint64},
	{name: "saturated", exponent: math.MaxUint64, value: math.MaxUint64, skipDesired: true},
}

var priceTowardCases = []towardCase[PriceExponent]{
	{name: "nil_unchanged", current: 1 << 40, noDesired: true, want: 1 << 40},
	{name: "no_change", current: 0, desired: 0, want: 0},
	{name: "increase_within_cap", current: 1000, desired: 2000, want: 2000},
	{name: "decrease_within_cap", current: 2000, desired: 1000, want: 1000},
	{name: "increase_at_cap", current: 0, desired: maxPriceDiff, want: maxPriceDiff},
	{name: "decrease_at_cap", current: maxPriceDiff, desired: 0, want: 0},
	{name: "increase_capped", current: 0, desired: maxPriceDiff + 1, want: maxPriceDiff},
	{name: "decrease_capped", current: 2 * maxPriceDiff, desired: 0, want: maxPriceDiff},
}

func TestPrice(t *testing.T) {
	testReader(t, priceReaderCases, PriceExponent.Price)
}

func TestDesiredPriceExponent(t *testing.T) {
	testDesired(t, priceReaderCases, DesiredPriceExponent)
}

func TestPriceExponentToward(t *testing.T) {
	testToward(t, priceTowardCases, PriceExponent.Toward)
}

func FuzzPriceExponentToward(f *testing.F) {
	for _, c := range priceTowardCases {
		if !c.noDesired {
			f.Add(uint64(c.current), uint64(c.desired))
		}
	}
	f.Fuzz(func(t *testing.T, current, desired uint64) {
		fuzzToward(t, PriceExponent(current), PriceExponent(desired), PriceExponent.Toward)
	})
}

func FuzzDesiredPriceExponent(f *testing.F) {
	for _, c := range priceReaderCases {
		f.Add(uint64(c.value))
	}
	f.Fuzz(func(t *testing.T, price uint64) {
		fuzzDesired(t, gas.Price(price), DesiredPriceExponent, PriceExponent.Price)
	})
}
