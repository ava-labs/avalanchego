// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/ava-labs/avalanchego/utils/math"
)

// OptimalParameters calculates the optimal [numSeeds] and [numBytes] that
// should be allocated for a bloom filter which will contain [maxEntries] and
// target [falsePositiveProbability].
func OptimalParameters(maxEntries uint64, falsePositiveProbability float64) (int, int) {
	optimalNumBits := bloomfilter.OptimalM(maxEntries, falsePositiveProbability)
	numBytes := (optimalNumBits + bitsPerByte - 1) / bitsPerByte
	numBytes = math.Max(numBytes, minEntries)
	numBits := numBytes * bitsPerByte

	numSeeds := bloomfilter.OptimalK(numBits, maxEntries)
	numSeeds = math.Max(numSeeds, minSeeds)
	numSeeds = math.Min(numSeeds, maxSeeds)
	return int(numSeeds), int(numBytes)
}
