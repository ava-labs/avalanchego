// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/utils/set"
)

func TestFindIndex(t *testing.T) {
	require := require.New(t)
	rand.Seed(1337)

	for i := 0; i < 1000; i++ {
		numWeights := rand.Intn(100) + 1 // #nosec G404
		cumulativeWeights := make([]uint64, numWeights)
		for i := range cumulativeWeights {
			nextWeight := rand.Intn(100) // #nosec G404
			if i == 0 {
				cumulativeWeights[i] = uint64(nextWeight)
			} else {
				cumulativeWeights[i] = cumulativeWeights[i-1] + uint64(nextWeight)
			}
		}
		weight := uint64(rand.Intn(int(cumulativeWeights[numWeights-1]))) // #nosec G404
		index := findIndex(weight, cumulativeWeights)
		cumulativeWeightAtIndex := cumulativeWeights[index]

		require.Greater(cumulativeWeightAtIndex, weight)
		first := slices.Index(cumulativeWeights, cumulativeWeightAtIndex)
		require.Equal(index, first)
		if index != 0 {
			require.GreaterOrEqual(weight, cumulativeWeights[index-1])
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
			// TODO remove
			t.Log(i, j)
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
	require.ErrorIs(err, errZeroWeight)

	// Test that initialize returns an error if len(weights) is 0.
	err = s.initialize([]uint64{})
	require.ErrorIs(err, errNoWeights)
}

// Test that the observed distribution of samples is close to the expected distribution.
func TestSamplerCorrectDistribution(t *testing.T) {
	require := require.New(t)
	rand.Seed(1337)

	s := &weightedSampler{}
	weights := []uint64{10, 5, 3, 2}
	sumWeights := 0
	for _, weight := range weights {
		sumWeights += int(weight)
	}
	err := s.initialize(weights)
	require.NoError(err)

	numSamples := 10_000

	// Case where we sample 1 element.
	{
		// Index --> number of draws of that index
		draws := map[int]int{}
		for i := 0; i < numSamples; i++ {
			sampled, err := s.sample(1)
			require.NoError(err)
			draws[sampled[0]]++
		}

		observedDrawProbabilites := []float64{}
		for i := 0; i < len(weights); i++ {
			observedDrawProbabilites = append(observedDrawProbabilites, float64(draws[i])/float64(numSamples))
		}
		expectedDrawProbabilities := []float64{}
		for _, weight := range weights {
			expectedDrawProbabilities = append(expectedDrawProbabilities, float64(weight)/float64(sumWeights))
		}
		require.InDeltaSlice(expectedDrawProbabilities, observedDrawProbabilites, 0.01)
	}

	// Case where we sample > 1 elements.
	// Test that the conditional probability of drawing a given index
	// given that the first element drawn is 0 is correct.
	draws := map[int]int{}
	adjustedSamples := 0
	for i := 0; i < numSamples; i++ {
		sampled, err := s.sample(2)
		require.NoError(err)
		if sampled[0] == 0 {
			draws[sampled[1]]++
			adjustedSamples++
		}
	}
	observedDrawProbabilites := []float64{}
	for i := 0; i < len(weights); i++ {
		observedDrawProbabilites = append(observedDrawProbabilites, float64(draws[i])/float64(adjustedSamples))
	}

	expectedDrawProbabilities := []float64{0}
	sumWeightsLessFirstElt := float64(sumWeights - int(weights[0]))
	for _, weight := range weights[1:] {
		expectedDrawProbabilities = append(expectedDrawProbabilities, float64(weight)/sumWeightsLessFirstElt)
	}

	require.InDeltaSlice(expectedDrawProbabilities, observedDrawProbabilites, 0.01)
}
