// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

const largestFloat64LessThan1 float64 = 1 - 1e-16

func TestOptimalHashes(t *testing.T) {
	tests := []struct {
		numEntries     int
		count          int
		expectedHashes int
	}{
		{ // invalid params
			numEntries:     0,
			count:          1024,
			expectedHashes: minHashes,
		},
		{ // invalid params
			numEntries:     1024,
			count:          0,
			expectedHashes: maxHashes,
		},
		{
			numEntries:     math.MaxInt,
			count:          1,
			expectedHashes: maxHashes,
		},
		{
			numEntries:     1,
			count:          math.MaxInt,
			expectedHashes: minHashes,
		},
		{
			numEntries:     1024,
			count:          1024,
			expectedHashes: 6,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d", test.numEntries, test.count), func(t *testing.T) {
			hashes := OptimalHashes(test.numEntries, test.count)
			require.Equal(t, test.expectedHashes, hashes)
		})
	}
}

func TestOptimalEntries(t *testing.T) {
	tests := []struct {
		count                    int
		falsePositiveProbability float64
		expectedEntries          int
	}{
		{ // invalid params
			count:                    0,
			falsePositiveProbability: .5,
			expectedEntries:          minEntries,
		},
		{ // invalid params
			count:                    1,
			falsePositiveProbability: 0,
			expectedEntries:          math.MaxInt,
		},
		{ // invalid params
			count:                    1,
			falsePositiveProbability: 1,
			expectedEntries:          minEntries,
		},
		{
			count:                    math.MaxInt,
			falsePositiveProbability: math.SmallestNonzeroFloat64,
			expectedEntries:          math.MaxInt,
		},
		{
			count:                    1024,
			falsePositiveProbability: largestFloat64LessThan1,
			expectedEntries:          minEntries,
		},
		{
			count:                    1024,
			falsePositiveProbability: .01,
			expectedEntries:          1227,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%f", test.count, test.falsePositiveProbability), func(t *testing.T) {
			entries := OptimalEntries(test.count, test.falsePositiveProbability)
			require.Equal(t, test.expectedEntries, entries)
		})
	}
}

func TestEstimateEntries(t *testing.T) {
	tests := []struct {
		numHashes                int
		numEntries               int
		falsePositiveProbability float64
		expectedEntries          int
	}{
		{ // invalid params
			numHashes:                0,
			numEntries:               2_048,
			falsePositiveProbability: .5,
			expectedEntries:          0,
		},
		{ // invalid params
			numHashes:                1,
			numEntries:               0,
			falsePositiveProbability: .5,
			expectedEntries:          0,
		},
		{ // invalid params
			numHashes:                1,
			numEntries:               1,
			falsePositiveProbability: 2,
			expectedEntries:          math.MaxInt,
		},
		{ // invalid params
			numHashes:                1,
			numEntries:               1,
			falsePositiveProbability: -1,
			expectedEntries:          0,
		},
		{
			numHashes:                8,
			numEntries:               2_048,
			falsePositiveProbability: 0,
			expectedEntries:          0,
		},
		{ // params from OptimalParameters(10_000, .01)
			numHashes:                7,
			numEntries:               11_982,
			falsePositiveProbability: .01,
			expectedEntries:          9_993,
		},
		{ // params from OptimalParameters(100_000, .001)
			numHashes:                10,
			numEntries:               179_720,
			falsePositiveProbability: .001,
			expectedEntries:          100_000,
		},
		{ // params from OptimalParameters(10_000, .01)
			numHashes:                7,
			numEntries:               11_982,
			falsePositiveProbability: .05,
			expectedEntries:          14_449,
		},
		{ // params from OptimalParameters(10_000, .01)
			numHashes:                7,
			numEntries:               11_982,
			falsePositiveProbability: 1,
			expectedEntries:          math.MaxInt,
		},
		{ // params from OptimalParameters(10_000, .01)
			numHashes:                7,
			numEntries:               11_982,
			falsePositiveProbability: math.SmallestNonzeroFloat64,
			expectedEntries:          0,
		},
		{ // params from OptimalParameters(10_000, .01)
			numHashes:                7,
			numEntries:               11_982,
			falsePositiveProbability: largestFloat64LessThan1,
			expectedEntries:          math.MaxInt,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d_%f", test.numHashes, test.numEntries, test.falsePositiveProbability), func(t *testing.T) {
			entries := EstimateCount(test.numHashes, test.numEntries, test.falsePositiveProbability)
			require.Equal(t, test.expectedEntries, entries)
		})
	}
}

func FuzzOptimalHashes(f *testing.F) {
	f.Fuzz(func(t *testing.T, numEntries, count int) {
		hashes := OptimalHashes(numEntries, count)
		require.GreaterOrEqual(t, hashes, minHashes)
		require.LessOrEqual(t, hashes, maxHashes)
	})
}

func FuzzOptimalEntries(f *testing.F) {
	f.Fuzz(func(t *testing.T, count int, falsePositiveProbability float64) {
		entries := OptimalEntries(count, falsePositiveProbability)
		require.GreaterOrEqual(t, entries, minEntries)
	})
}

func FuzzEstimateEntries(f *testing.F) {
	f.Fuzz(func(t *testing.T, numHashes, numEntries int, falsePositiveProbability float64) {
		entries := EstimateCount(numHashes, numEntries, falsePositiveProbability)
		require.GreaterOrEqual(t, entries, 0)
	})
}
