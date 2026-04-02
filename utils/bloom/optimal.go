// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import "math"

const ln2Squared = math.Ln2 * math.Ln2

// OptimalParameters calculates the optimal [numHashes] and [numEntries] that
// should be allocated for a bloom filter which will contain [count] and target
// [falsePositiveProbability].
func OptimalParameters(count int, falsePositiveProbability float64) (int, int) {
	numEntries := OptimalEntries(count, falsePositiveProbability)
	numHashes := OptimalHashes(numEntries, count)
	return numHashes, numEntries
}

// OptimalHashes calculates the number of hashes which will minimize the false
// positive probability of a bloom filter with [numEntries] after [count]
// additions.
//
// It is guaranteed to return a value in the range [minHashes, maxHashes].
//
// ref: https://en.wikipedia.org/wiki/Bloom_filter
func OptimalHashes(numEntries, count int) int {
	switch {
	case numEntries < minEntries:
		return minHashes
	case count <= 0:
		return maxHashes
	}

	numHashes := math.Ceil(float64(numEntries) * bitsPerByte * math.Ln2 / float64(count))
	// Converting a floating-point value to an int produces an undefined value
	// if the floating-point value cannot be represented as an int. To avoid
	// this undefined behavior, we explicitly check against MaxInt here.
	//
	// ref: https://go.dev/ref/spec#Conversions
	if numHashes >= maxHashes {
		return maxHashes
	}
	return max(int(numHashes), minHashes)
}

// OptimalEntries calculates the optimal number of entries to use when creating
// a new Bloom filter when targenting a size of [count] with
// [falsePositiveProbability] assuming that the optimal number of hashes is
// used.
//
// It is guaranteed to return a value in the range [minEntries, MaxInt].
//
// ref: https://en.wikipedia.org/wiki/Bloom_filter
func OptimalEntries(count int, falsePositiveProbability float64) int {
	switch {
	case count <= 0:
		return minEntries
	case falsePositiveProbability >= 1:
		return minEntries
	case falsePositiveProbability <= 0:
		return math.MaxInt
	}

	entriesInBits := -float64(count) * math.Log(falsePositiveProbability) / ln2Squared
	entries := (entriesInBits + bitsPerByte - 1) / bitsPerByte
	// Converting a floating-point value to an int produces an undefined value
	// if the floating-point value cannot be represented as an int. To avoid
	// this undefined behavior, we explicitly check against MaxInt here.
	//
	// ref: https://go.dev/ref/spec#Conversions
	if entries >= math.MaxInt {
		return math.MaxInt
	}
	return max(int(entries), minEntries)
}

// EstimateCount estimates the number of additions a bloom filter with
// [numHashes] and [numEntries] must have to reach [falsePositiveProbability].
// This is derived by inversing a lower-bound on the probability of false
// positives. For values where numBits >> numHashes, the predicted probability
// is fairly accurate.
//
// It is guaranteed to return a value in the range [0, MaxInt].
//
// ref: https://tsapps.nist.gov/publication/get_pdf.cfm?pub_id=903775
func EstimateCount(numHashes, numEntries int, falsePositiveProbability float64) int {
	switch {
	case numHashes < minHashes:
		return 0
	case numEntries < minEntries:
		return 0
	case falsePositiveProbability <= 0:
		return 0
	case falsePositiveProbability >= 1:
		return math.MaxInt
	}

	invNumHashes := 1 / float64(numHashes)
	numBits := float64(numEntries * 8)
	exp := 1 - math.Pow(falsePositiveProbability, invNumHashes)
	count := math.Ceil(-math.Log(exp) * numBits * invNumHashes)
	// Converting a floating-point value to an int produces an undefined value
	// if the floating-point value cannot be represented as an int. To avoid
	// this undefined behavior, we explicitly check against MaxInt here.
	//
	// ref: https://go.dev/ref/spec#Conversions
	if count >= math.MaxInt {
		return math.MaxInt
	}
	return int(count)
}
