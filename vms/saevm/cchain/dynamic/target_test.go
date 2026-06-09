// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// targetReaderCases pin TargetExponent to its Target() value. The ln argument
// is the target divided by the minimum.
var targetReaderCases = []readerCase[TargetExponent, gas.Gas]{
	{name: "zero", exponent: 0, value: 1_000_000},
	{name: "largest_unchanged", exponent: 33, value: 1_000_000, skipDesired: true},
	{name: "smallest_change", exponent: 34, value: 1_000_001},
	{name: "max_step", exponent: 1 << 15, value: 1_000_977, skipDesired: true},
	{name: "1.5m", exponent: 13_605_152, value: 1_500_000},                                      // 2^25 * ln(1.5)
	{name: "3m", exponent: 36_863_312, value: 3_000_000},                                        // 2^25 * ln(3)
	{name: "6m", exponent: 60_121_472, value: 6_000_000},                                        // 2^25 * ln(6)
	{name: "10m", exponent: 77_261_935, value: 10_000_000},                                      // 2^25 * ln(10)
	{name: "100m", exponent: 154_523_870, value: 100_000_000},                                   // 2^25 * ln(100)
	{name: "low_1b", exponent: 231_785_804, value: 1_000_000_000 - 24},                          // 2^25 * ln(1000)
	{name: "high_1b", exponent: 231_785_805, value: 1_000_000_000 + 6},                          // 2^25 * ln(1000) + 1
	{name: "largest_capacity", exponent: 947_688_691, value: 1_844_674_384_269_701_322},         // 2^25 * ln(MaxUint64 / MinMaxCapacity)
	{name: "largest_int64", exponent: 1_001_692_466, value: 9_223_371_923_824_614_091},          // 2^25 * ln(MaxInt64 / minimum)
	{name: "second_largest_uint64", exponent: 1_024_950_626, value: 18_446_743_882_783_898_031}, // 2^25 * ln(MaxUint64 / minimum)
	{name: "largest_uint64", exponent: 1_024_950_627, value: math.MaxUint64},
	{name: "saturated", exponent: math.MaxUint64, value: math.MaxUint64, skipDesired: true},
}

var targetTowardCases = []towardCase[TargetExponent]{
	{name: "nil_unchanged", current: 13_605_152, want: 13_605_152},
	{name: "no_change", current: 0, desired: utils.PointerTo[TargetExponent](0), want: 0},
	{name: "increase_within_cap", current: 1000, desired: utils.PointerTo[TargetExponent](2000), want: 2000},
	{name: "decrease_within_cap", current: 2000, desired: utils.PointerTo[TargetExponent](1000), want: 1000},
	{name: "increase_at_cap", current: 0, desired: utils.PointerTo[TargetExponent](1 << 15), want: 1 << 15},
	{name: "decrease_at_cap", current: 1 << 15, desired: utils.PointerTo[TargetExponent](0), want: 0},
	{name: "increase_capped", current: 0, desired: utils.PointerTo[TargetExponent]((1 << 15) + 1), want: 1 << 15},
	{name: "decrease_capped", current: 1_000_000, desired: utils.PointerTo[TargetExponent](0), want: 1_000_000 - (1 << 15)},
}

func TestTarget(t *testing.T) {
	testReader(t, targetReaderCases, TargetExponent.Target)
}

func TestDesiredTargetExponent(t *testing.T) {
	testSearch(t, targetReaderCases, DesiredTargetExponent)
}

func TestTargetExponentToward(t *testing.T) {
	testToward(t, targetTowardCases, TargetExponent.Toward)
}

func FuzzTargetExponentToward(f *testing.F) {
	fuzzToward(f, targetTowardCases, TargetExponent.Toward)
}

func FuzzDesiredTargetExponent(f *testing.F) {
	fuzzSearch(f, targetReaderCases, DesiredTargetExponent, TargetExponent.Target)
}
