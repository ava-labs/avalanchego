// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/utils"
)

var (
	uniformSamplers = []struct {
		name    string
		sampler Uniform
	}{
		{
			name:    "replacer",
			sampler: &uniformReplacer{},
		},
		{
			name:    "resampler",
			sampler: &uniformResample{},
		},
		{
			name:    "best",
			sampler: NewBestUniform(30),
		},
	}
	uniformTests = []struct {
		name string
		test func(*testing.T, Uniform)
	}{
		{
			name: "initialize overflow",
			test: UniformInitializeOverflowTest,
		},
		{
			name: "out of range",
			test: UniformOutOfRangeTest,
		},
		{
			name: "empty",
			test: UniformEmptyTest,
		},
		{
			name: "singleton",
			test: UniformSingletonTest,
		},
		{
			name: "distribution",
			test: UniformDistributionTest,
		},
		{
			name: "over sample",
			test: UniformOverSampleTest,
		},
		{
			name: "lazily sample",
			test: UniformLazilySample,
		},
	}
)

func TestAllUniform(t *testing.T) {
	for _, s := range uniformSamplers {
		for _, test := range uniformTests {
			t.Run(fmt.Sprintf("sampler %s test %s", s.name, test.name), func(t *testing.T) {
				test.test(t, s.sampler)
			})
		}
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

func UniformLazilySample(t *testing.T, s Uniform) {
	err := s.Initialize(3)
	assert.NoError(t, err)

	for j := 0; j < 2; j++ {
		sampled := map[uint64]bool{}
		for i := 0; i < 3; i++ {
			val, err := s.Next()
			assert.NoError(t, err)
			assert.False(t, sampled[val])

			sampled[val] = true
		}

		_, err = s.Next()
		assert.Error(t, err, "should have returned an out of range error")

		s.Reset()
	}
}

func TestSeeding(t *testing.T) {
	assert := assert.New(t)

	s1 := NewBestUniform(30)
	s2 := NewBestUniform(30)

	err := s1.Initialize(50)
	assert.NoError(err)

	err = s2.Initialize(50)
	assert.NoError(err)

	s1.Seed(0)

	s1.Reset()
	s1Val, err := s1.Next()
	assert.NoError(err)

	s2.Seed(1)
	s2.Reset()

	s1.Seed(0)
	v, err := s2.Next()
	assert.NoError(err)
	assert.NotEqualValues(s1Val, v)

	s1.ClearSeed()

	_, err = s1.Next()
	assert.NoError(err)
}

func TestSeedingProducesTheSame(t *testing.T) {
	assert := assert.New(t)

	s := NewBestUniform(30)

	err := s.Initialize(50)
	assert.NoError(err)

	s.Seed(0)
	s.Reset()

	val0, err := s.Next()
	assert.NoError(err)

	s.Seed(0)
	s.Reset()

	val1, err := s.Next()
	assert.NoError(err)
	assert.Equal(val0, val1)
}
