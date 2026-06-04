// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"math"
	"testing"
)

// Independent copies of the implementation's constants. Sharing them would make
// the assertions tautological.
const (
	minDelay         = 1                         // milliseconds
	maxDelayDiff     = 200                       // DelayExponent.Toward step cap
	maxDelayExponent = DelayExponent(46_516_320) // saturates Delay to MaxUint64
)

// delayReaderCases are reader vectors ported from the ACP-226 reference tests.
var delayReaderCases = []readerCase[DelayExponent, uint64]{
	{name: "zero", exponent: 0, value: minDelay},
	{name: "smallest_change", exponent: 726_820, value: minDelay + 1},
	{name: "max_step", exponent: maxDelayDiff, value: 1, skipDesired: true},
	{name: "100ms", exponent: 4_828_872, value: 100},                                // 2^20 * ln(100) + 2
	{name: "500ms", exponent: 6_516_490, value: 500},                                // 2^20 * ln(500) + 2
	{name: "1000ms", exponent: 7_243_307, value: 1000},                              // 2^20 * ln(1000) + 1
	{name: "2000ms", exponent: 7_970_124, value: 2000},                              // 2^20 * ln(2000) + 1, the ACP-226 initial seed
	{name: "5000ms", exponent: 8_930_925, value: 5000},                              // 2^20 * ln(5000) + 1
	{name: "10000ms", exponent: 9_657_742, value: 10000},                            // 2^20 * ln(10000) + 1
	{name: "60000ms", exponent: 11_536_538, value: 60000},                           // 2^20 * ln(60000) + 1
	{name: "300000ms", exponent: 13_224_156, value: 300000},                         // 2^20 * ln(300000) + 1
	{name: "largest_int64", exponent: 45_789_502, value: 9_223_368_741_047_657_702}, // 2^20 * ln(MaxInt64)
	{name: "second_largest_uint64", exponent: maxDelayExponent - 1, value: 18_446_728_723_565_431_225},
	{name: "largest_uint64", exponent: maxDelayExponent, value: math.MaxUint64},
	{name: "saturated", exponent: math.MaxUint64, value: math.MaxUint64, skipDesired: true},
}

var delayTowardCases = []towardCase[DelayExponent]{
	{name: "nil_unchanged", current: 1234, noDesired: true, want: 1234},
	{name: "no_change", current: 0, desired: 0, want: 0},
	{name: "increase_within_cap", current: 50, desired: 100, want: 100},
	{name: "decrease_within_cap", current: 100, desired: 50, want: 50},
	{name: "increase_at_cap", current: 0, desired: maxDelayDiff, want: maxDelayDiff},
	{name: "decrease_at_cap", current: maxDelayDiff, desired: 0, want: 0},
	{name: "increase_capped", current: 0, desired: 1000, want: maxDelayDiff},
	{name: "decrease_capped", current: 1000, desired: 0, want: 1000 - maxDelayDiff},
}

func TestDelay(t *testing.T) {
	testReader(t, delayReaderCases, DelayExponent.Delay)
}

func TestDesiredDelayExponent(t *testing.T) {
	testDesired(t, delayReaderCases, DesiredDelayExponent)
}

func TestDelayExponentToward(t *testing.T) {
	testToward(t, delayTowardCases, DelayExponent.Toward)
}

func FuzzDelayExponentToward(f *testing.F) {
	for _, c := range delayTowardCases {
		if !c.noDesired {
			f.Add(uint64(c.current), uint64(c.desired))
		}
	}
	f.Fuzz(func(t *testing.T, current, desired uint64) {
		fuzzToward(t, DelayExponent(current), DelayExponent(desired), DelayExponent.Toward)
	})
}

func FuzzDesiredDelayExponent(f *testing.F) {
	for _, c := range delayReaderCases {
		f.Add(c.value)
	}
	f.Fuzz(func(t *testing.T, delay uint64) {
		fuzzDesired(t, delay, DesiredDelayExponent, DelayExponent.Delay)
	})
}
