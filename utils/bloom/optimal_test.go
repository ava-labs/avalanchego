// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

const largestFloat64LessThan1 float64 = 1 - 1e-16

func TestEstimateEntries(t *testing.T) {
	tests := []struct {
		numSeeds                 int
		numBytes                 int
		falsePositiveProbability float64
		expectedEntries          int
	}{
		{ // invalid params
			numSeeds:                 0,
			numBytes:                 2_048,
			falsePositiveProbability: .5,
			expectedEntries:          0,
		},
		{ // invalid params
			numSeeds:                 1,
			numBytes:                 0,
			falsePositiveProbability: .5,
			expectedEntries:          0,
		},
		{ // invalid params
			numSeeds:                 1,
			numBytes:                 1,
			falsePositiveProbability: 2,
			expectedEntries:          math.MaxInt,
		},
		{ // invalid params
			numSeeds:                 1,
			numBytes:                 1,
			falsePositiveProbability: -1,
			expectedEntries:          0,
		},
		{
			numSeeds:                 8,
			numBytes:                 2_048,
			falsePositiveProbability: 0,
			expectedEntries:          0,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: .01,
			expectedEntries:          9_993,
		},
		{ // params from OptimalParameters(100_000, .001)
			numSeeds:                 10,
			numBytes:                 179_720,
			falsePositiveProbability: .001,
			expectedEntries:          100_000,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: .05,
			expectedEntries:          14_449,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: 1,
			expectedEntries:          math.MaxInt,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: math.SmallestNonzeroFloat64,
			expectedEntries:          0,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: largestFloat64LessThan1,
			expectedEntries:          math.MaxInt,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d_%f", test.numSeeds, test.numBytes, test.falsePositiveProbability), func(t *testing.T) {
			entries := EstimateEntries(test.numSeeds, test.numBytes, test.falsePositiveProbability)
			require.Equal(t, test.expectedEntries, entries)
		})
	}
}

func TestEstimateEntriesSteps(t *testing.T) {
	tests := []struct {
		numSeeds                 int
		numBytes                 int
		falsePositiveProbability float64
		expectedEntries          int
	}{
		{
			numSeeds:                 8,
			numBytes:                 2_048,
			falsePositiveProbability: 0,
			expectedEntries:          0,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: .01,
			expectedEntries:          9_993,
		},
		{ // params from OptimalParameters(100_000, .001)
			numSeeds:                 10,
			numBytes:                 179_720,
			falsePositiveProbability: .001,
			expectedEntries:          100_000,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: .05,
			expectedEntries:          14_449,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: 1,
			expectedEntries:          math.MaxInt,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: math.SmallestNonzeroFloat64,
			expectedEntries:          0,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                 7,
			numBytes:                 11_982,
			falsePositiveProbability: largestFloat64LessThan1,
			expectedEntries:          math.MaxInt,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d_%f", test.numSeeds, test.numBytes, test.falsePositiveProbability), func(t *testing.T) {
			invNumSeeds := 1 / float64(test.numSeeds)
			if invNumSeeds < 0 {
				t.Fatal("invNumSeeds is negative")
			}
			if invNumSeeds > 1 {
				t.Fatal("invNumSeeds is greater than 1")
			}
			numBits := float64(test.numBytes * 8)
			exp := 1 - math.Pow(test.falsePositiveProbability, invNumSeeds)
			if exp < 0 {
				t.Fatal("exp is negative")
			}
			if exp > 1 {
				t.Fatal("exp is greater than 1")
			}
			entries := -math.Log(exp) * numBits * invNumSeeds
			if entries < 0 {
				t.Fatal("entries is negative")
			}
		})
	}
}
