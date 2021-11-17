// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"math"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	weightedWithoutReplacementSamplers = []struct {
		name    string
		sampler WeightedWithoutReplacement
	}{
		{
			name: "generic with replacer and best",
			sampler: &weightedWithoutReplacementGeneric{
				u: &uniformReplacer{},
				w: &weightedBest{
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
		},
	}
	weightedWithoutReplacementTests = []struct {
		name string
		test func(*testing.T, WeightedWithoutReplacement)
	}{
		{
			name: "initialize overflow",
			test: WeightedWithoutReplacementInitializeOverflowTest,
		},
		{
			name: "out of range",
			test: WeightedWithoutReplacementOutOfRangeTest,
		},
		{
			name: "empty without weight",
			test: WeightedWithoutReplacementEmptyWithoutWeightTest,
		},
		{
			name: "empty",
			test: WeightedWithoutReplacementEmptyTest,
		},
		{
			name: "singleton",
			test: WeightedWithoutReplacementSingletonTest,
		},
		{
			name: "with zero",
			test: WeightedWithoutReplacementWithZeroTest,
		},
		{
			name: "distribution",
			test: WeightedWithoutReplacementDistributionTest,
		},
	}
)

func TestAllWeightedWithoutReplacement(t *testing.T) {
	for _, s := range weightedWithoutReplacementSamplers {
		for _, test := range weightedWithoutReplacementTests {
			t.Run(fmt.Sprintf("sampler %s test %s", s.name, test.name), func(t *testing.T) {
				test.test(t, s.sampler)
			})
		}
	}
}

func WeightedWithoutReplacementInitializeOverflowTest(
	t *testing.T,
	s WeightedWithoutReplacement,
) {
	err := s.Initialize([]uint64{1, math.MaxUint64})
	assert.Error(t, err, "should have reported an overflow error")
}

func WeightedWithoutReplacementOutOfRangeTest(
	t *testing.T,
	s WeightedWithoutReplacement,
) {
	err := s.Initialize([]uint64{1})
	assert.NoError(t, err)

	_, err = s.Sample(2)
	assert.Error(t, err, "should have reported an out of range error")
}

func WeightedWithoutReplacementEmptyWithoutWeightTest(
	t *testing.T,
	s WeightedWithoutReplacement,
) {
	err := s.Initialize(nil)
	assert.NoError(t, err)

	indices, err := s.Sample(0)
	assert.NoError(t, err)
	assert.Len(t, indices, 0, "shouldn't have selected any elements")
}

func WeightedWithoutReplacementEmptyTest(
	t *testing.T,
	s WeightedWithoutReplacement,
) {
	err := s.Initialize([]uint64{1})
	assert.NoError(t, err)

	indices, err := s.Sample(0)
	assert.NoError(t, err)
	assert.Len(t, indices, 0, "shouldn't have selected any elements")
}

func WeightedWithoutReplacementSingletonTest(
	t *testing.T,
	s WeightedWithoutReplacement,
) {
	err := s.Initialize([]uint64{1})
	assert.NoError(t, err)

	indices, err := s.Sample(1)
	assert.NoError(t, err)
	assert.Equal(t, []int{0}, indices, "should have selected the first element")
}

func WeightedWithoutReplacementWithZeroTest(
	t *testing.T,
	s WeightedWithoutReplacement,
) {
	err := s.Initialize([]uint64{0, 1})
	assert.NoError(t, err)

	indices, err := s.Sample(1)
	assert.NoError(t, err)
	assert.Equal(
		t,
		[]int{1},
		indices,
		"should have selected the second element",
	)
}

func WeightedWithoutReplacementDistributionTest(
	t *testing.T,
	s WeightedWithoutReplacement,
) {
	err := s.Initialize([]uint64{1, 1, 2})
	assert.NoError(t, err)

	indices, err := s.Sample(4)
	assert.NoError(t, err)

	sort.Ints(indices)
	assert.Equal(
		t,
		[]int{0, 1, 2, 2},
		indices,
		"should have selected all the elements",
	)
}
