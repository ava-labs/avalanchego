// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"math"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/stretchr/testify/require"
)

func TestFindIndex(t *testing.T) {
	require := require.New(t)
	rand.Seed(1337)

	for i := 0; i < 1000; i++ {
		numWeights := rand.Intn(100) + 1 // #nosec G404
		cumulativeWeights := make([]uint64, numWeights)
		for i := range cumulativeWeights {
			nextWeight := rand.Intn(100) + 1 // #nosec G404
			if i == 0 {
				cumulativeWeights[i] = uint64(nextWeight)
			} else {
				cumulativeWeights[i] = cumulativeWeights[i-1] + uint64(nextWeight)
			}
		}
		weight := uint64(rand.Intn(int(cumulativeWeights[numWeights-1]))) // #nosec G404
		index := findIndex(weight, cumulativeWeights)
		require.LessOrEqual(weight, cumulativeWeights[index])
		if index != 0 {
			require.Greater(weight, cumulativeWeights[index-1])
		}
	}
}

// Test that sample always returns a unique list.
func TestSamplerUnique(t *testing.T) {
	require := require.New(t)
	rand.Seed(1337)
	s := &weightedSampler{}

	for i := 0; i < 100; i++ {
		// Generate a random list of weights and initialize the sampler.
		numWeights := 1 + rand.Intn(100) // #nosec G404
		weights := make([]uint64, numWeights)
		for i := 0; i < numWeights; i++ {
			weights[i] = uint64(rand.Intn(100) + 1) // #nosec G404
		}
		err := s.initialize(weights)
		require.NoError(err)

		for j := 0; j < 100; j++ {
			// Sample a random number of elements.
			numToSample := rand.Intn(numWeights) // #nosec G404
			sampled, err := s.sample(numToSample)
			require.NoError(err)
			require.Len(sampled, numToSample)

			// Assert that the returned elements are unique.
			seen := set.Set[int]{}
			for _, elt := range sampled {
				require.False(seen.Contains(elt))
				seen.Add(elt)
			}
		}
	}
}

func TestSamplerInitialize(t *testing.T) {
	require := require.New(t)
	s := &weightedSampler{}

	// Test that initialize returns an error if the sum of weights is too large.
	err := s.initialize([]uint64{math.MaxInt64 - 1, 2})
	require.ErrorIs(err, errWeightTooLarge)

	// Test that sum(weights) == math.MaxInt64 is OK
	err = s.initialize([]uint64{math.MaxInt - 1})
	require.NoError(err)

	// Test that initialize returns an error if the sum of weights is 0.
	err = s.initialize([]uint64{0, 0})
	require.ErrorIs(err, errNoWeights)

	// Test that initialize returns an error if len(weights) is 0.
	err = s.initialize([]uint64{})
	require.ErrorIs(err, errNoWeights)
}
