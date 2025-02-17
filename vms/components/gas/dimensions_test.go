// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gas

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func Test_Dimensions_Add(t *testing.T) {
	tests := []struct {
		name        string
		lhs         Dimensions
		rhs         []*Dimensions
		expected    Dimensions
		expectedErr error
	}{
		{
			name: "no error single entry",
			lhs: Dimensions{
				Bandwidth: 1,
				DBRead:    2,
				DBWrite:   3,
				Compute:   4,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 10,
					DBRead:    20,
					DBWrite:   30,
					Compute:   40,
				},
			},
			expected: Dimensions{
				Bandwidth: 11,
				DBRead:    22,
				DBWrite:   33,
				Compute:   44,
			},
			expectedErr: nil,
		},
		{
			name: "no error multiple entries",
			lhs: Dimensions{
				Bandwidth: 1,
				DBRead:    2,
				DBWrite:   3,
				Compute:   4,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 10,
					DBRead:    20,
					DBWrite:   30,
					Compute:   40,
				},
				{
					Bandwidth: 100,
					DBRead:    200,
					DBWrite:   300,
					Compute:   400,
				},
			},
			expected: Dimensions{
				Bandwidth: 111,
				DBRead:    222,
				DBWrite:   333,
				Compute:   444,
			},
			expectedErr: nil,
		},
		{
			name: "bandwidth overflow",
			lhs: Dimensions{
				Bandwidth: math.MaxUint64,
				DBRead:    2,
				DBWrite:   3,
				Compute:   4,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 10,
					DBRead:    20,
					DBWrite:   30,
					Compute:   40,
				},
			},
			expected: Dimensions{
				Bandwidth: 0,
				DBRead:    2,
				DBWrite:   3,
				Compute:   4,
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "db read overflow",
			lhs: Dimensions{
				Bandwidth: 1,
				DBRead:    math.MaxUint64,
				DBWrite:   3,
				Compute:   4,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 10,
					DBRead:    20,
					DBWrite:   30,
					Compute:   40,
				},
			},
			expected: Dimensions{
				Bandwidth: 11,
				DBRead:    0,
				DBWrite:   3,
				Compute:   4,
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "db write overflow",
			lhs: Dimensions{
				Bandwidth: 1,
				DBRead:    2,
				DBWrite:   math.MaxUint64,
				Compute:   4,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 10,
					DBRead:    20,
					DBWrite:   30,
					Compute:   40,
				},
			},
			expected: Dimensions{
				Bandwidth: 11,
				DBRead:    22,
				DBWrite:   0,
				Compute:   4,
			},
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "compute overflow",
			lhs: Dimensions{
				Bandwidth: 1,
				DBRead:    2,
				DBWrite:   3,
				Compute:   math.MaxUint64,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 10,
					DBRead:    20,
					DBWrite:   30,
					Compute:   40,
				},
			},
			expected: Dimensions{
				Bandwidth: 11,
				DBRead:    22,
				DBWrite:   33,
				Compute:   0,
			},
			expectedErr: safemath.ErrOverflow,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := test.lhs.Add(test.rhs...)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}

func Test_Dimensions_Sub(t *testing.T) {
	tests := []struct {
		name        string
		lhs         Dimensions
		rhs         []*Dimensions
		expected    Dimensions
		expectedErr error
	}{
		{
			name: "no error single entry",
			lhs: Dimensions{
				Bandwidth: 11,
				DBRead:    22,
				DBWrite:   33,
				Compute:   44,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 1,
					DBRead:    2,
					DBWrite:   3,
					Compute:   4,
				},
			},
			expected: Dimensions{
				Bandwidth: 10,
				DBRead:    20,
				DBWrite:   30,
				Compute:   40,
			},
			expectedErr: nil,
		},
		{
			name: "no error multiple entries",
			lhs: Dimensions{
				Bandwidth: 11,
				DBRead:    22,
				DBWrite:   33,
				Compute:   44,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 1,
					DBRead:    2,
					DBWrite:   3,
					Compute:   4,
				},
				{
					Bandwidth: 5,
					DBRead:    5,
					DBWrite:   5,
					Compute:   5,
				},
			},
			expected: Dimensions{
				Bandwidth: 5,
				DBRead:    15,
				DBWrite:   25,
				Compute:   35,
			},
			expectedErr: nil,
		},
		{
			name: "bandwidth underflow",
			lhs: Dimensions{
				Bandwidth: 11,
				DBRead:    22,
				DBWrite:   33,
				Compute:   44,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: math.MaxUint64,
					DBRead:    2,
					DBWrite:   3,
					Compute:   4,
				},
			},
			expected: Dimensions{
				Bandwidth: 0,
				DBRead:    22,
				DBWrite:   33,
				Compute:   44,
			},
			expectedErr: safemath.ErrUnderflow,
		},
		{
			name: "db read underflow",
			lhs: Dimensions{
				Bandwidth: 11,
				DBRead:    22,
				DBWrite:   33,
				Compute:   44,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 1,
					DBRead:    math.MaxUint64,
					DBWrite:   3,
					Compute:   4,
				},
			},
			expected: Dimensions{
				Bandwidth: 10,
				DBRead:    0,
				DBWrite:   33,
				Compute:   44,
			},
			expectedErr: safemath.ErrUnderflow,
		},
		{
			name: "db write underflow",
			lhs: Dimensions{
				Bandwidth: 11,
				DBRead:    22,
				DBWrite:   33,
				Compute:   44,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 1,
					DBRead:    2,
					DBWrite:   math.MaxUint64,
					Compute:   4,
				},
			},
			expected: Dimensions{
				Bandwidth: 10,
				DBRead:    20,
				DBWrite:   0,
				Compute:   44,
			},
			expectedErr: safemath.ErrUnderflow,
		},
		{
			name: "compute underflow",
			lhs: Dimensions{
				Bandwidth: 11,
				DBRead:    22,
				DBWrite:   33,
				Compute:   44,
			},
			rhs: []*Dimensions{
				{
					Bandwidth: 1,
					DBRead:    2,
					DBWrite:   3,
					Compute:   math.MaxUint64,
				},
			},
			expected: Dimensions{
				Bandwidth: 10,
				DBRead:    20,
				DBWrite:   30,
				Compute:   0,
			},
			expectedErr: safemath.ErrUnderflow,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := test.lhs.Sub(test.rhs...)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}

func Test_Dimensions_ToGas(t *testing.T) {
	tests := []struct {
		name        string
		units       Dimensions
		weights     Dimensions
		expected    Gas
		expectedErr error
	}{
		{
			name: "no error",
			units: Dimensions{
				Bandwidth: 1,
				DBRead:    2,
				DBWrite:   3,
				Compute:   4,
			},
			weights: Dimensions{
				Bandwidth: 1000,
				DBRead:    100,
				DBWrite:   10,
				Compute:   1,
			},
			expected:    1*1000 + 2*100 + 3*10 + 4*1,
			expectedErr: nil,
		},
		{
			name: "multiplication overflow",
			units: Dimensions{
				Bandwidth: 2,
				DBRead:    1,
				DBWrite:   1,
				Compute:   1,
			},
			weights: Dimensions{
				Bandwidth: math.MaxUint64,
				DBRead:    1,
				DBWrite:   1,
				Compute:   1,
			},
			expected:    0,
			expectedErr: safemath.ErrOverflow,
		},
		{
			name: "addition overflow",
			units: Dimensions{
				Bandwidth: 1,
				DBRead:    1,
				DBWrite:   0,
				Compute:   0,
			},
			weights: Dimensions{
				Bandwidth: math.MaxUint64,
				DBRead:    math.MaxUint64,
				DBWrite:   1,
				Compute:   1,
			},
			expected:    0,
			expectedErr: safemath.ErrOverflow,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			actual, err := test.units.ToGas(test.weights)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)

			actual, err = test.weights.ToGas(test.units)
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expected, actual)
		})
	}
}

func Benchmark_Dimensions_Add(b *testing.B) {
	lhs := Dimensions{600, 10, 10, 1000}
	rhs := []*Dimensions{
		{1, 1, 1, 1},
		{10, 10, 10, 10},
		{100, 100, 100, 100},
		{200, 200, 200, 200},
		{500, 500, 500, 500},
		{1_000, 1_000, 1_000, 1_000},
		{10_000, 10_000, 10_000, 10_000},
	}

	b.Run("single", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = lhs.Add(rhs[0])
		}
	})

	b.Run("multiple", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = lhs.Add(rhs[0], rhs[1], rhs[2], rhs[3], rhs[4], rhs[5], rhs[6])
		}
	})
}

func Benchmark_Dimensions_Sub(b *testing.B) {
	lhs := Dimensions{10_000, 10_000, 10_000, 100_000}
	rhs := []*Dimensions{
		{1, 1, 1, 1},
		{10, 10, 10, 10},
		{100, 100, 100, 100},
		{200, 200, 200, 200},
		{500, 500, 500, 500},
		{1_000, 1_000, 1_000, 1_000},
	}

	b.Run("single", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = lhs.Sub(rhs[0])
		}
	})

	b.Run("multiple", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = lhs.Sub(rhs[0], rhs[1], rhs[2], rhs[3], rhs[4], rhs[5])
		}
	})
}
