// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/utils"
)

// delayReaderCases pin DelayExponent to its Delay() value, including the
// ACP-226 initial seed and the saturating boundaries.
var delayReaderCases = []readerCase[DelayExponent, uint64]{
	{name: "zero", exponent: 0, value: 1},
	{name: "smallest_change", exponent: 726_820, value: 2},
	{name: "max_step", exponent: 200, value: 1, skipDesired: true},
	{name: "100ms", exponent: 4_828_872, value: 100},                                // 2^20 * ln(100) + 2
	{name: "500ms", exponent: 6_516_490, value: 500},                                // 2^20 * ln(500) + 2
	{name: "1000ms", exponent: 7_243_307, value: 1000},                              // 2^20 * ln(1000) + 1
	{name: "2000ms", exponent: 7_970_124, value: 2000},                              // 2^20 * ln(2000) + 1, the ACP-226 initial seed
	{name: "5000ms", exponent: 8_930_925, value: 5000},                              // 2^20 * ln(5000) + 1
	{name: "10000ms", exponent: 9_657_742, value: 10000},                            // 2^20 * ln(10000) + 1
	{name: "60000ms", exponent: 11_536_538, value: 60000},                           // 2^20 * ln(60000) + 1
	{name: "300000ms", exponent: 13_224_156, value: 300000},                         // 2^20 * ln(300000) + 1
	{name: "largest_int64", exponent: 45_789_502, value: 9_223_368_741_047_657_702}, // 2^20 * ln(MaxInt64)
	{name: "second_largest_uint64", exponent: 46_516_319, value: 18_446_728_723_565_431_225},
	{name: "largest_uint64", exponent: 46_516_320, value: math.MaxUint64},
	{name: "saturated", exponent: math.MaxUint64, value: math.MaxUint64, skipDesired: true},
}

var delayTowardCases = []towardCase[DelayExponent]{
	{name: "nil_unchanged", current: 1234, want: 1234},
	{name: "no_change", current: 0, desired: utils.PointerTo[DelayExponent](0), want: 0},
	{name: "increase_within_cap", current: 50, desired: utils.PointerTo[DelayExponent](100), want: 100},
	{name: "decrease_within_cap", current: 100, desired: utils.PointerTo[DelayExponent](50), want: 50},
	{name: "increase_at_cap", current: 0, desired: utils.PointerTo[DelayExponent](200), want: 200},
	{name: "decrease_at_cap", current: 200, desired: utils.PointerTo[DelayExponent](0), want: 0},
	{name: "increase_capped", current: 0, desired: utils.PointerTo[DelayExponent](1000), want: 200},
	{name: "decrease_capped", current: 1000, desired: utils.PointerTo[DelayExponent](0), want: 800},
}

func TestDelay(t *testing.T) {
	testReader(t, delayReaderCases, DelayExponent.Delay)
}

func TestDesiredDelayExponent(t *testing.T) {
	testSearch(t, delayReaderCases, DesiredDelayExponent)
}

func TestDelayExponentToward(t *testing.T) {
	testToward(t, delayTowardCases, DelayExponent.Toward)
}

func FuzzDelayExponentToward(f *testing.F) {
	fuzzToward(f, delayTowardCases, DelayExponent.Toward)
}

func FuzzDesiredDelayExponent(f *testing.F) {
	fuzzSearch(f, delayReaderCases, DesiredDelayExponent, DelayExponent.Delay)
}
