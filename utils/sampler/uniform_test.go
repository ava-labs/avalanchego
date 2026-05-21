// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUniformInitializeMaxUint64(t *testing.T) {
	s := NewUniform()
	s.Initialize(math.MaxUint64)

	for {
		val, hasNext := s.Next()
		require.True(t, hasNext)

		if val > math.MaxInt64 {
			break
		}
	}
}

func TestUniformOutOfRange(t *testing.T) {
	s := NewUniform()
	s.Initialize(0)

	_, ok := s.Sample(1)
	require.False(t, ok)
}

func TestUniformEmpty(t *testing.T) {
	require := require.New(t)
	s := NewUniform()

	s.Initialize(1)

	val, ok := s.Sample(0)
	require.True(ok)
	require.Empty(val)
}

func TestUniformSingleton(t *testing.T) {
	require := require.New(t)
	s := NewUniform()

	s.Initialize(1)

	val, ok := s.Sample(1)
	require.True(ok)
	require.Equal([]uint64{0}, val)
}

func TestUniformDistribution(t *testing.T) {
	require := require.New(t)
	s := NewUniform()

	s.Initialize(3)

	val, ok := s.Sample(3)
	require.True(ok)

	slices.Sort(val)
	require.Equal([]uint64{0, 1, 2}, val)
}

func TestUniformOverSample(t *testing.T) {
	s := NewUniform()
	s.Initialize(3)

	_, ok := s.Sample(4)
	require.False(t, ok)
}

func TestUniformLazilySample(t *testing.T) {
	require := require.New(t)
	s := NewUniform()

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
