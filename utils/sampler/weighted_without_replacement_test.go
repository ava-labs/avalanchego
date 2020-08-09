// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	weightedWithoutReplacementTests = []func(
		*testing.T,
		WeightedWithoutReplacement,
	){
		WeightedWithoutReplacementInitializeOverflowTest,
		WeightedWithoutReplacementOutOfRangeTest,
		WeightedWithoutReplacementEmptyWithoutWeightTest,
		WeightedWithoutReplacementEmptyTest,
		WeightedWithoutReplacementSingletonTest,
		WeightedWithoutReplacementWithZeroTest,
		WeightedWithoutReplacementDistributionTest,
	}
)

func WeightedWithoutReplacementTest(
	t *testing.T,
	s WeightedWithoutReplacement,
) {
	for _, test := range weightedWithoutReplacementTests {
		test(t, s)
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
