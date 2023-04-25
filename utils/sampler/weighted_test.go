// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"testing"

	stdmath "math"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/math"
)

var (
	weightedSamplers = []struct {
		name    string
		sampler Weighted
	}{
		{
			name:    "inverse uniform cdf",
			sampler: &weightedArray{},
		},
		{
			name:    "heap division",
			sampler: &weightedHeap{},
		},
		{
			name:    "linear scan",
			sampler: &weightedLinear{},
		},
		{
			name: "lookup",
			sampler: &weightedUniform{
				maxWeight: 1024,
			},
		},
		{
			name: "best with k=30",
			sampler: &weightedBest{
				samplers: []Weighted{
					&weightedArray{},
					&weightedHeap{},
					&weightedUniform{
						maxWeight: 1024,
					},
				},
				benchmarkIterations: 30,
			},
		},
	}
	weightedTests = []struct {
		name string
		test func(*testing.T, Weighted)
	}{
		{
			name: "initialize overflow",
			test: WeightedInitializeOverflowTest,
		},
		{
			name: "out of range",
			test: WeightedOutOfRangeTest,
		},
		{
			name: "singleton",
			test: WeightedSingletonTest,
		},
		{
			name: "with zero",
			test: WeightedWithZeroTest,
		},
		{
			name: "distribution",
			test: WeightedDistributionTest,
		},
	}
)

func TestAllWeighted(t *testing.T) {
	for _, s := range weightedSamplers {
		for _, test := range weightedTests {
			t.Run(fmt.Sprintf("sampler %s test %s", s.name, test.name), func(t *testing.T) {
				test.test(t, s.sampler)
			})
		}
	}
}

func WeightedInitializeOverflowTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1, stdmath.MaxUint64})
	require.ErrorIs(t, err, math.ErrOverflow)
}

func WeightedOutOfRangeTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1})
	require.NoError(t, err)

	_, err = s.Sample(1)
	require.ErrorIs(t, err, ErrOutOfRange)
}

func WeightedSingletonTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1})
	require.NoError(t, err)

	index, err := s.Sample(0)
	require.NoError(t, err)
	require.Equal(t, 0, index, "should have selected the first element")
}

func WeightedWithZeroTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{0, 1})
	require.NoError(t, err)

	index, err := s.Sample(0)
	require.NoError(t, err)
	require.Equal(t, 1, index, "should have selected the second element")
}

func WeightedDistributionTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1, 1, 2, 3, 4})
	require.NoError(t, err)

	counts := make([]int, 5)
	for i := uint64(0); i < 11; i++ {
		index, err := s.Sample(i)
		require.NoError(t, err)
		counts[index]++
	}
	require.Equal(t, []int{1, 1, 2, 3, 4}, counts, "wrong distribution returned")
}
