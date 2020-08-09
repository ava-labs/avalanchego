// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"testing"

	"github.com/ava-labs/gecko/utils"
	"github.com/stretchr/testify/assert"
)

var (
	uniformTests = []func(*testing.T, Uniform){
		UniformInitializeOverflowTest,
		UniformOutOfRangeTest,
		UniformEmptyTest,
		UniformSingletonTest,
		UniformDistributionTest,
		UniformOverSampleTest,
	}
)

func UniformTest(t *testing.T, s Uniform) {
	for _, test := range uniformTests {
		test(t, s)
	}
}

func UniformInitializeOverflowTest(t *testing.T, s Uniform) {
	err := s.Initialize(math.MaxUint64)
	assert.Error(t, err, "should have reported an overflow error")
}

func UniformOutOfRangeTest(t *testing.T, s Uniform) {
	err := s.Initialize(0)
	assert.NoError(t, err)

	_, err = s.Sample(1)
	assert.Error(t, err, "should have reported an out of range error")
}

func UniformEmptyTest(t *testing.T, s Uniform) {
	err := s.Initialize(1)
	assert.NoError(t, err)

	val, err := s.Sample(0)
	assert.NoError(t, err)
	assert.Len(t, val, 0, "shouldn't have selected any element")
}

func UniformSingletonTest(t *testing.T, s Uniform) {
	err := s.Initialize(1)
	assert.NoError(t, err)

	val, err := s.Sample(1)
	assert.NoError(t, err)
	assert.Equal(t, []uint64{0}, val, "should have selected the only element")
}

func UniformDistributionTest(t *testing.T, s Uniform) {
	err := s.Initialize(3)
	assert.NoError(t, err)

	val, err := s.Sample(3)
	assert.NoError(t, err)

	utils.SortUint64(val)
	assert.Equal(
		t,
		[]uint64{0, 1, 2},
		val,
		"should have selected the only element",
	)
}

func UniformOverSampleTest(t *testing.T, s Uniform) {
	err := s.Initialize(3)
	assert.NoError(t, err)

	_, err = s.Sample(4)
	assert.Error(t, err, "should have returned an out of range error")
}
