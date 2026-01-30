// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestWeightedWithoutReplacementInitializeOverflow(t *testing.T) {
	s := NewWeightedWithoutReplacement()
	err := s.Initialize([]uint64{1, math.MaxUint64})
	require.ErrorIs(t, err, safemath.ErrOverflow)
}

func TestWeightedWithoutReplacementOutOfRange(t *testing.T) {
	require := require.New(t)
	s := NewWeightedWithoutReplacement()

	require.NoError(s.Initialize([]uint64{1}))

	_, ok := s.Sample(2)
	require.False(ok)
}

func TestWeightedWithoutReplacementEmptyWithoutWeight(t *testing.T) {
	require := require.New(t)
	s := NewWeightedWithoutReplacement()

	require.NoError(s.Initialize(nil))

	indices, ok := s.Sample(0)
	require.True(ok)
	require.Empty(indices)
}

func TestWeightedWithoutReplacementEmpty(t *testing.T) {
	require := require.New(t)
	s := NewWeightedWithoutReplacement()

	require.NoError(s.Initialize([]uint64{1}))

	indices, ok := s.Sample(0)
	require.True(ok)
	require.Empty(indices)
}

func TestWeightedWithoutReplacementSingleton(t *testing.T) {
	require := require.New(t)
	s := NewWeightedWithoutReplacement()

	require.NoError(s.Initialize([]uint64{1}))

	indices, ok := s.Sample(1)
	require.True(ok)
	require.Equal([]int{0}, indices)
}

func TestWeightedWithoutReplacementWithZero(t *testing.T) {
	require := require.New(t)
	s := NewWeightedWithoutReplacement()

	require.NoError(s.Initialize([]uint64{0, 1}))

	indices, ok := s.Sample(1)
	require.True(ok)
	require.Equal([]int{1}, indices)
}

func TestWeightedWithoutReplacementDistribution(t *testing.T) {
	require := require.New(t)
	s := NewWeightedWithoutReplacement()

	require.NoError(s.Initialize([]uint64{1, 1, 2}))

	indices, ok := s.Sample(4)
	require.True(ok)

	slices.Sort(indices)
	require.Equal([]int{0, 1, 2, 2}, indices)
}
