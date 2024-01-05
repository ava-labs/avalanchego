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
	switch {
	case numSeeds < minSeeds:
		return 0
	case numBytes < minEntries:
		return 0
	case falsePositiveProbability <= 0:
		return 0
	case falsePositiveProbability >= 1:
		return math.MaxInt
	}

	invNumSeeds := 1 / float64(numSeeds)
	numBits := float64(numBytes * 8)
	exp := 1 - math.Pow(falsePositiveProbability, invNumSeeds)
	entries := math.Ceil(-math.Log(exp) * numBits * invNumSeeds)
	// Converting a floating-point value to an int produces an undefined value
	// if the floating-point value cannot be represented as an int. To avoid
	// this undefined behavior, we explicitly check against MaxInt here.
	//
	// ref: https://go.dev/ref/spec#Conversions
	if entries >= math.MaxInt {
		return math.MaxInt
	}
	return int(entries)
}
