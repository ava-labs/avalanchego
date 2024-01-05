// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"math"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// OptimalParameters calculates the optimal [numSeeds] and [numBytes] that
// should be allocated for a bloom filter which will contain [maxEntries] and
// target [falsePositiveProbability].
func OptimalParameters(maxEntries int, falsePositiveProbability float64) (int, int) {
	optimalNumBits := bloomfilter.OptimalM(uint64(maxEntries), falsePositiveProbability)
	numBytes := (optimalNumBits + bitsPerByte - 1) / bitsPerByte
	numBytes = safemath.Max(numBytes, minEntries)
	numBits := numBytes * bitsPerByte

	numSeeds := bloomfilter.OptimalK(numBits, uint64(maxEntries))
	numSeeds = safemath.Max(numSeeds, minSeeds)
	numSeeds = safemath.Min(numSeeds, maxSeeds)
	return int(numSeeds), int(numBytes)
}

// EstimateEntries estimates the number of entries that must be added to a bloom
// filter with [numSeeds] and [numBytes] to reach [falsePositiveProbability].
// This is derived by inversing a lower-bound on the probability of false
// positives. For values where numBits >> numSeeds, the predicted probability is
// fairly accurate.
//
// ref: https://tsapps.nist.gov/publication/get_pdf.cfm?pub_id=903775
func EstimateEntries(numSeeds, numBytes int, falsePositiveProbability float64) int {
	invNumSeeds := 1 / float64(numSeeds)
	numBits := float64(numBytes * 8)
	exp := 1 - math.Pow(falsePositiveProbability, invNumSeeds)
	entries := -math.Log(exp) * numBits * invNumSeeds
	return int(math.Ceil(entries))
}
