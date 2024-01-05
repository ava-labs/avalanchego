// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEstimateEntries(t *testing.T) {
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
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d_%f", test.numSeeds, test.numBytes, test.falsePositiveProbability), func(t *testing.T) {
			entries := EstimateEntries(test.numSeeds, test.numBytes, test.falsePositiveProbability)
			require.Equal(t, test.expectedEntries, entries)
		})
	}
}
