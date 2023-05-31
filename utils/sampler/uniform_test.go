// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"golang.org/x/exp/slices"
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
			name: "can sample large values",
			test: UniformInitializeMaxUint64Test,
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

func UniformInitializeMaxUint64Test(t *testing.T, s Uniform) {
	s.Initialize(math.MaxUint64)

	for {
		val, err := s.Next()
		require.NoError(t, err)

		if val > math.MaxInt64 {
			break
		}
	}
}

func UniformOutOfRangeTest(t *testing.T, s Uniform) {
	s.Initialize(0)

	_, err := s.Sample(1)
	require.ErrorIs(t, err, ErrOutOfRange)
}

func UniformEmptyTest(t *testing.T, s Uniform) {
	s.Initialize(1)

	val, err := s.Sample(0)
	require.NoError(t, err)
	require.Empty(t, val)
}

func UniformSingletonTest(t *testing.T, s Uniform) {
	s.Initialize(1)

	val, err := s.Sample(1)
	require.NoError(t, err)
	require.Equal(t, []uint64{0}, val, "should have selected the only element")
}

func UniformDistributionTest(t *testing.T, s Uniform) {
	s.Initialize(3)

	val, err := s.Sample(3)
	require.NoError(t, err)

	slices.Sort(val)
	require.Equal(
		t,
		[]uint64{0, 1, 2},
		val,
		"should have selected the only element",
	)
}

func UniformOverSampleTest(t *testing.T, s Uniform) {
	s.Initialize(3)

	_, err := s.Sample(4)
	require.ErrorIs(t, err, ErrOutOfRange)
}

func UniformLazilySample(t *testing.T, s Uniform) {
	s.Initialize(3)

	for j := 0; j < 2; j++ {
		sampled := map[uint64]bool{}
		for i := 0; i < 3; i++ {
			val, err := s.Next()
			require.NoError(t, err)
			require.False(t, sampled[val])

			sampled[val] = true
		}

		_, err := s.Next()
		require.ErrorIs(t, err, ErrOutOfRange)

		s.Reset()
	}
}

func TestSeeding(t *testing.T) {
	require := require.New(t)

	s1 := NewBestUniform(30)
	s2 := NewBestUniform(30)

	s1.Initialize(50)
	s2.Initialize(50)

	s1.Seed(0)

	s1.Reset()
	s1Val, err := s1.Next()
	require.NoError(err)

	s2.Seed(1)
	s2.Reset()

	s1.Seed(0)
	v, err := s2.Next()
	require.NoError(err)
	require.NotEqual(s1Val, v)

	s1.ClearSeed()

	_, err = s1.Next()
	require.NoError(err)
}

func TestSeedingProducesTheSame(t *testing.T) {
	require := require.New(t)

	s := NewBestUniform(30)

	s.Initialize(50)

	s.Seed(0)
	s.Reset()

	val0, err := s.Next()
	require.NoError(err)

	s.Seed(0)
	s.Reset()

	val1, err := s.Next()
	require.NoError(err)
	require.Equal(val0, val1)
}
