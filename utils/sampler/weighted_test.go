// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func TestWeightedInitializeOverflow(t *testing.T) {
	s := &weightedHeap{}
	err := s.Initialize([]uint64{1, math.MaxUint64})
	require.ErrorIs(t, err, safemath.ErrOverflow)
}

func TestWeightedOutOfRange(t *testing.T) {
	require := require.New(t)
	s := &weightedHeap{}

	require.NoError(s.Initialize([]uint64{1}))

	_, ok := s.Sample(1)
	require.False(ok)
}

func TestWeightedSingleton(t *testing.T) {
	require := require.New(t)
	s := &weightedHeap{}

	require.NoError(s.Initialize([]uint64{1}))

	index, ok := s.Sample(0)
	require.True(ok)
	require.Zero(index)
}

func TestWeightedWithZero(t *testing.T) {
	require := require.New(t)
	s := &weightedHeap{}

	require.NoError(s.Initialize([]uint64{0, 1}))

	index, ok := s.Sample(0)
	require.True(ok)
	require.Equal(1, index)
}

func TestWeightedDistribution(t *testing.T) {
	require := require.New(t)
	s := &weightedHeap{}

	require.NoError(s.Initialize([]uint64{1, 1, 2, 3, 4}))

	counts := make([]int, 5)
	for i := uint64(0); i < 11; i++ {
		index, ok := s.Sample(i)
		require.True(ok)
		counts[index]++
	}
	require.Equal([]int{1, 1, 2, 3, 4}, counts)
}
