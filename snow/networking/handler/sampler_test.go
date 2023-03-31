// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/stretchr/testify/require"
)

// Test that sample always returns a unique list.
func TestSamplerUnique(t *testing.T) {
	require := require.New(t)
	rand.Seed(1337)
	s := &weightedSampler{}

	for i := 0; i < 100; i++ {
		// Generate a random list of weights and initialize the sampler.
		numWeights := 1 + rand.Intn(100) // Add 1 to avoid call to rand.Intn(0) which panics.
		weightsBytes := make([]byte, numWeights)
		_, err := rand.Read(weightsBytes)
		require.NoError(err)
		weights := make([]uint64, numWeights)
		for i, b := range weightsBytes {
			weights[i] = uint64(b)
		}
		err = s.initialize(weights)
		require.NoError(err)

		for j := 0; j < 100; j++ {
			// Sample a random number of elements.
			numToSample := rand.Intn(numWeights)
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

// func TestSamplerInitialize(t *testing.T) {
// 	require := require.New(t)
// 	s := &weightedSampler{}

// 	// Test that initialize returns an error if the sum of weights is too large.
// 	err := s.initialize([]uint64{math.MaxInt64 - 1, 2})
// 	require.ErrorIs(err, errWeightTooLarge)

// 	// Test that sum(weights) == math.MaxInt64 is OK
// 	err = s.initialize([]uint64{math.MaxInt - 1})
// 	require.NoError(err)

// 	// Test that initialize returns an error if the sum of weights is 0.
// 	err = s.initialize([]uint64{0, 0})
// 	require.ErrorIs(err, errNoWeights)

// 	// Test that initialize returns an error if len(weights) is 0.
// 	err = s.initialize([]uint64{})
// 	require.ErrorIs(err, errNoWeights)
// }
