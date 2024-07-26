// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

var gasPriceMulExpTests = []struct {
	minPrice                 GasPrice
	excess                   Gas
	excessConversionConstant Gas
	expected                 GasPrice
}{
	{
		minPrice:                 1,
		excess:                   0,
		excessConversionConstant: 1,
		expected:                 1,
	},
	{
		minPrice:                 1,
		excess:                   1,
		excessConversionConstant: 1,
		expected:                 2,
	},
	{
		minPrice:                 1,
		excess:                   2,
		excessConversionConstant: 1,
		expected:                 6,
	},
	{
		minPrice:                 1,
		excess:                   10_000,
		excessConversionConstant: 10_000,
		expected:                 2,
	},
	{
		minPrice:                 1,
		excess:                   1_000_000,
		excessConversionConstant: 10_000,
		expected:                 math.MaxUint64,
	},
	{
		minPrice:                 10,
		excess:                   10_000_000,
		excessConversionConstant: 1_000_000,
		expected:                 220_264,
	},
	{
		minPrice:                 math.MaxUint64,
		excess:                   math.MaxUint64,
		excessConversionConstant: 1,
		expected:                 math.MaxUint64,
	},
	{
		minPrice:                 math.MaxUint32,
		excess:                   1,
		excessConversionConstant: 1,
		expected:                 11_674_931_546,
	},
	{
		minPrice:                 6_786_177_901_268_885_274, // ~ MaxUint64 / e
		excess:                   1,
		excessConversionConstant: 1,
		expected:                 math.MaxUint64 - 11,
	},
	{
		minPrice:                 6_786_177_901_268_885_274, // ~ MaxUint64 / e
		excess:                   math.MaxUint64,
		excessConversionConstant: math.MaxUint64,
		expected:                 math.MaxUint64 - 1,
	},
}

func Test_Gas_AddPerSecond(t *testing.T) {
	tests := []struct {
		initial      Gas
		gasPerSecond Gas
		seconds      uint64
		expected     Gas
	}{
		{
			initial:      5,
			gasPerSecond: 1,
			seconds:      2,
			expected:     7,
		},
		{
			initial:      5,
			gasPerSecond: math.MaxUint64,
			seconds:      2,
			expected:     math.MaxUint64,
		},
		{
			initial:      math.MaxUint64,
			gasPerSecond: 1,
			seconds:      2,
			expected:     math.MaxUint64,
		},
		{
			initial:      math.MaxUint64,
			gasPerSecond: math.MaxUint64,
			seconds:      math.MaxUint64,
			expected:     math.MaxUint64,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d+%d*%d=%d", test.initial, test.gasPerSecond, test.seconds, test.expected), func(t *testing.T) {
			actual := test.initial.AddPerSecond(test.gasPerSecond, test.seconds)
			require.Equal(t, test.expected, actual)
		})
	}
}

func Test_Gas_SubPerSecond(t *testing.T) {
	tests := []struct {
		initial      Gas
		gasPerSecond Gas
		seconds      uint64
		expected     Gas
	}{
		{
			initial:      5,
			gasPerSecond: 1,
			seconds:      2,
			expected:     3,
		},
		{
			initial:      5,
			gasPerSecond: math.MaxUint64,
			seconds:      2,
			expected:     0,
		},
		{
			initial:      1,
			gasPerSecond: 1,
			seconds:      2,
			expected:     0,
		},
		{
			initial:      math.MaxUint64,
			gasPerSecond: math.MaxUint64,
			seconds:      math.MaxUint64,
			expected:     0,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d-%d*%d=%d", test.initial, test.gasPerSecond, test.seconds, test.expected), func(t *testing.T) {
			actual := test.initial.SubPerSecond(test.gasPerSecond, test.seconds)
			require.Equal(t, test.expected, actual)
		})
	}
}

func Test_GasPrice_MulExp(t *testing.T) {
	for _, test := range gasPriceMulExpTests {
		t.Run(fmt.Sprintf("%d*e^(%d/%d)=%d", test.minPrice, test.excess, test.excessConversionConstant, test.expected), func(t *testing.T) {
			actual := test.minPrice.MulExp(test.excess, test.excessConversionConstant)
			require.Equal(t, test.expected, actual)
		})
	}
}

func Benchmark_GasPrice_MulExp(b *testing.B) {
	for _, test := range gasPriceMulExpTests {
		b.Run(fmt.Sprintf("%d*e^(%d/%d)=%d", test.minPrice, test.excess, test.excessConversionConstant, test.expected), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				test.minPrice.MulExp(test.excess, test.excessConversionConstant)
			}
		})
	}
}
