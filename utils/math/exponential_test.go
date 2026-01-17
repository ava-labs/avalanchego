// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApproximateExponential(t *testing.T) {
	tests := []struct {
		name        string
		factor      uint64
		numerator   uint64
		denominator uint64
		want        uint64
	}{
		{
			name:        "zero factor",
			factor:      0,
			numerator:   1,
			denominator: 1,
			want:        0,
		},
		{
			name:        "all zeros",
			factor:      0,
			numerator:   0,
			denominator: 1,
			want:        0,
		},
		{
			name:        "zero numerator",
			factor:      100,
			numerator:   0,
			denominator: 1,
			want:        100,
		},
		{
			name:        "identity - factor=1, num=0",
			factor:      1,
			numerator:   0,
			denominator: 1,
			want:        1,
		},
		{
			name:        "e^1 approximation",
			factor:      1,
			numerator:   1,
			denominator: 1,
			want:        2, // e^1 ≈ 2.718, truncated to 2 with factor=1
		},
		{
			name:        "e^2 approximation",
			factor:      1,
			numerator:   2,
			denominator: 1,
			want:        6, // e^2 ≈ 7.389, truncated to 6 with factor=1
		},
		{
			name:        "small exponential growth",
			factor:      1,
			numerator:   1,
			denominator: 2,
			want:        1, // e^0.5 ≈ 1.648, but with factor=1 and integer math: 1
		},
		{
			name:        "exponential with larger factor",
			factor:      1000000,
			numerator:   1,
			denominator: 10,
			want:        1105170, // approximately 1000000 * e^0.1
		},
		{
			name:        "larger exponential",
			factor:      1000000,
			numerator:   5,
			denominator: 10,
			want:        1648721, // approximately 1000000 * e^0.5
		},
		{
			name:        "exponential equals 1",
			factor:      1000000,
			numerator:   10,
			denominator: 10,
			want:        2718281, // approximately 1000000 * e^1
		},
		{
			name:        "overflow - max factor with positive exponent",
			factor:      math.MaxUint64,
			numerator:   1,
			denominator: 1,
			want:        math.MaxUint64,
		},
		{
			name:        "overflow - large numerator",
			factor:      math.MaxUint64,
			numerator:   100,
			denominator: 1,
			want:        math.MaxUint64,
		},
		{
			name:        "overflow - large factor with large numerator",
			factor:      1 << 60,
			numerator:   100,
			denominator: 1,
			want:        math.MaxUint64,
		},
		{
			name:        "overflow - large factor and numerator",
			factor:      math.MaxUint64 / 2,
			numerator:   10,
			denominator: 1,
			want:        math.MaxUint64,
		},
		{
			name:        "overflow - moderate factor with extreme ratio",
			factor:      1000000,
			numerator:   1000,
			denominator: 1,
			want:        math.MaxUint64,
		},
		{
			name:        "high precision denominator",
			factor:      1000000000,
			numerator:   1,
			denominator: 1000000000,
			want:        1000000001, // approximately 1000000000 * e^(1/1000000000)
		},
		{
			name:        "max uint64 factor, zero numerator",
			factor:      math.MaxUint64,
			numerator:   0,
			denominator: 1,
			want:        math.MaxUint64,
		},
		{
			name:        "denominator larger than numerator",
			factor:      100000,
			numerator:   1,
			denominator: 100,
			want:        101005, // approximately 100000 * e^0.01
		},
		{
			name:        "very large denominator",
			factor:      1000000,
			numerator:   1,
			denominator: math.MaxUint64,
			want:        1000000, // virtually no change with such a large denominator
		},
		{
			name:        "realistic gas price scenario",
			factor:      25000000000, // 25 gwei
			numerator:   1000000,
			denominator: 3338477,
			want:        33730875609, // ~33.73 gwei after exponential increase
		},
		{
			name:        "small excess, large conversion",
			factor:      1000000,
			numerator:   1,
			denominator: 1000000,
			want:        1000001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApproximateExponential(tt.factor, tt.numerator, tt.denominator)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestApproximateExponentialMonotonicity(t *testing.T) {
	factor := uint64(1000000)
	denominator := uint64(100)

	prev := ApproximateExponential(factor, 0, denominator)
	for numerator := uint64(1); numerator < 10; numerator++ {
		curr := ApproximateExponential(factor, numerator, denominator)
		require.Greater(t, curr, prev, "result should increase as numerator increases")
		prev = curr
	}
}
