// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// priceReaderCases pin each price to its smallest yielding exponent, using
// values derived from the ACP-283 reference parameters.
var priceReaderCases = []readerCase[PriceExponent, gas.Price]{
	{name: "minimum", exponent: 0, value: 1},
	{name: "shared_minimum", exponent: 1, value: 1, skipDesired: true},
	{name: "double", exponent: 288_230_376_151_711_749, value: 2},            // conversionRate * ln(2)
	{name: "quadruple", exponent: 576_460_752_303_423_490, value: 4},         // conversionRate * ln(4)
	{name: "octuple", exponent: 864_691_128_455_135_233, value: 8},           // conversionRate * ln(8)
	{name: "hundred", exponent: 1_914_961_168_676_647_261, value: 100},       // conversionRate * ln(100)
	{name: "million", exponent: 5_744_883_506_029_941_780, value: 1_000_000}, // conversionRate * ln(1_000_000)
	{name: "2^40", exponent: 11_529_215_046_068_469_736, value: 1 << 40},     // conversionRate * ln(2^40)
	{name: "largest_uint64", exponent: math.MaxUint64 - 37, value: math.MaxUint64},
	{name: "saturated", exponent: math.MaxUint64, value: math.MaxUint64, skipDesired: true},
}

// priceMaxDiff is the per-step cap (288_230_376_151_711_744 / 3_600).
const priceMaxDiff = 80_063_993_375_475

var priceTowardCases = []towardCase[PriceExponent]{
	{name: "nil_unchanged", current: 1 << 40, want: 1 << 40},
	{name: "no_change", current: 0, desired: utils.PointerTo[PriceExponent](0), want: 0},
	{name: "increase_within_cap", current: 1000, desired: utils.PointerTo[PriceExponent](2000), want: 2000},
	{name: "decrease_within_cap", current: 2000, desired: utils.PointerTo[PriceExponent](1000), want: 1000},
	{name: "increase_at_cap", current: 0, desired: utils.PointerTo[PriceExponent](priceMaxDiff), want: priceMaxDiff},
	{name: "decrease_at_cap", current: priceMaxDiff, desired: utils.PointerTo[PriceExponent](0), want: 0},
	{name: "increase_capped", current: 0, desired: utils.PointerTo[PriceExponent](priceMaxDiff + 1), want: priceMaxDiff},
	{name: "decrease_capped", current: 2 * priceMaxDiff, desired: utils.PointerTo[PriceExponent](0), want: priceMaxDiff},
}

func TestPrice(t *testing.T) {
	testReader(t, priceReaderCases, PriceExponent.Price)
}

func TestDesiredPriceExponent(t *testing.T) {
	testSearch(t, priceReaderCases, DesiredPriceExponent)
}

func TestPriceExponentToward(t *testing.T) {
	testToward(t, priceTowardCases, PriceExponent.Toward)
}

func FuzzPriceExponentToward(f *testing.F) {
	fuzzToward(f, priceTowardCases, PriceExponent.Toward)
}

func FuzzDesiredPriceExponent(f *testing.F) {
	fuzzSearch(f, priceReaderCases, DesiredPriceExponent, PriceExponent.Price)
}
