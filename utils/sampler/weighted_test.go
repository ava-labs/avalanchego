// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	weightedTests = []func(*testing.T, Weighted){
		WeightedInitializeOverflowTest,
		WeightedOutOfRangeTest,
		WeightedSingletonTest,
		WeightedWithZeroTest,
		WeightedDistributionTest,
	}
)

func WeightedTest(t *testing.T, s Weighted) {
	for _, test := range weightedTests {
		test(t, s)
	}
}

func WeightedInitializeOverflowTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1, math.MaxUint64})
	assert.Error(t, err, "should have reported an overflow error")
}

func WeightedOutOfRangeTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1})
	assert.NoError(t, err)

	_, err = s.Sample(1)
	assert.Error(t, err, "should have reported an out of range error")
}

func WeightedSingletonTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1})
	assert.NoError(t, err)

	index, err := s.Sample(0)
	assert.NoError(t, err)
	assert.Equal(t, 0, index, "should have selected the first element")
}

func WeightedWithZeroTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{0, 1})
	assert.NoError(t, err)

	index, err := s.Sample(0)
	assert.NoError(t, err)
	assert.Equal(t, 1, index, "should have selected the second element")
}

func WeightedDistributionTest(t *testing.T, s Weighted) {
	err := s.Initialize([]uint64{1, 1, 2, 3, 4})
	assert.NoError(t, err)

	counts := make([]int, 5)
	for i := uint64(0); i < 11; i++ {
		index, err := s.Sample(i)
		assert.NoError(t, err)
		counts[index]++
	}
	assert.Equal(t, []int{1, 1, 2, 3, 4}, counts, "wrong distribution returned")
}
