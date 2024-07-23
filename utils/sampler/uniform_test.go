// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	uniformSamplers = []struct {
		name    string
		sampler Uniform
	}{
		{
			name: "replacer",
			sampler: &uniformReplacer{
				rng: globalRNG,
			},
		},
		{
			name: "resampler",
			sampler: &uniformResample{
				rng: globalRNG,
			},
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
		val, hasNext := s.Next()
		require.True(t, hasNext)

		if val > math.MaxInt64 {
			break
		}
	}
}

func UniformOutOfRangeTest(t *testing.T, s Uniform) {
	s.Initialize(0)

	_, ok := s.Sample(1)
	require.False(t, ok)
}

func UniformEmptyTest(t *testing.T, s Uniform) {
	require := require.New(t)

	s.Initialize(1)

	val, ok := s.Sample(0)
	require.True(ok)
	require.Empty(val)
}

func UniformSingletonTest(t *testing.T, s Uniform) {
	require := require.New(t)

	s.Initialize(1)

	val, ok := s.Sample(1)
	require.True(ok)
	require.Equal([]uint64{0}, val)
}

func UniformDistributionTest(t *testing.T, s Uniform) {
	require := require.New(t)

	s.Initialize(3)

	val, ok := s.Sample(3)
	require.True(ok)

	slices.Sort(val)
	require.Equal([]uint64{0, 1, 2}, val)
}

func UniformOverSampleTest(t *testing.T, s Uniform) {
	s.Initialize(3)

	_, ok := s.Sample(4)
	require.False(t, ok)
}

func UniformLazilySample(t *testing.T, s Uniform) {
	require := require.New(t)

	s.Initialize(3)

	for j := 0; j < 2; j++ {
		sampled := map[uint64]bool{}
		for i := 0; i < 3; i++ {
			val, hasNext := s.Next()
			require.True(hasNext)
			require.False(sampled[val])

			sampled[val] = true
		}

		_, hasNext := s.Next()
		require.False(hasNext)

		s.Reset()
	}
}
