// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	safemath "github.com/ava-labs/avalanchego/utils/math"
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
	err := s.Initialize([]uint64{1, math.MaxUint64})
	require.ErrorIs(t, err, safemath.ErrOverflow)
}

func WeightedOutOfRangeTest(t *testing.T, s Weighted) {
	require := require.New(t)

	require.NoError(s.Initialize([]uint64{1}))

	_, ok := s.Sample(1)
	require.False(ok)
}

func WeightedSingletonTest(t *testing.T, s Weighted) {
	require := require.New(t)

	require.NoError(s.Initialize([]uint64{1}))

	index, ok := s.Sample(0)
	require.True(ok)
	require.Zero(index)
}

func WeightedWithZeroTest(t *testing.T, s Weighted) {
	require := require.New(t)

	require.NoError(s.Initialize([]uint64{0, 1}))

	index, ok := s.Sample(0)
	require.True(ok)
	require.Equal(1, index)
}

func WeightedDistributionTest(t *testing.T, s Weighted) {
	require := require.New(t)

	require.NoError(s.Initialize([]uint64{1, 1, 2, 3, 4}))

	counts := make([]int, 5)
	for i := uint64(0); i < 11; i++ {
		index, ok := s.Sample(i)
		require.True(ok)
		counts[index]++
	}
	require.Equal([]int{1, 1, 2, 3, 4}, counts)
}
