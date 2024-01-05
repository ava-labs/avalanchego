// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"crypto/rand"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// NewBloomFilter returns a new instance of a bloom filter with at least [minTargetElements] elements
// anticipated at any moment, and a false positive probability of [falsePositiveProbability].
//
// Invariant: The returned bloom filter is not safe to reset concurrently with
// other operations. However, it is otherwise safe to access concurrently.
func NewBloomFilter(
	minTargetElements int,
	falsePositiveProbability float64,
) (*BloomFilter, error) {
	numHashes, numEntries := bloom.OptimalParameters(
		minTargetElements,
		falsePositiveProbability,
	)
	b, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return nil, err
	}

	salt, err := randomSalt()
	return &BloomFilter{
		minTargetElements:        minTargetElements,
		falsePositiveProbability: falsePositiveProbability,
		maxCount:                 bloom.EstimateCount(numHashes, numEntries, falsePositiveProbability),
		bloom:                    b,
		salt:                     salt,
	}, err
}

type BloomFilter struct {
	minTargetElements        int
	falsePositiveProbability float64

	maxCount int
	bloom    *bloom.Filter
	// salt is provided to eventually unblock collisions in Bloom. It's possible
	// that conflicting Gossipable items collide in the bloom filter, so a salt
	// is generated to eventually resolve collisions.
	salt ids.ID
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	h := gossipable.GossipID()
	bloom.Add(b.bloom, h[:], b.salt[:])
}

func (b *BloomFilter) Has(gossipable Gossipable) bool {
	h := gossipable.GossipID()
	return bloom.Contains(b.bloom, h[:], b.salt[:])
}

func (b *BloomFilter) Marshal() ([]byte, []byte) {
	bloomBytes := b.bloom.Marshal()
	// salt must be copied here to ensure the bytes aren't overwritten if salt
	// is later modified.
	salt := b.salt
	return bloomBytes, salt[:]
}

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches [falsePositiveProbability].
//
// If the number of elements that are tracked with the bloom filter exceeds [minTargetElements], the size
// of the bloom filter will grow to maintain the same [falsePositiveProbability].
//
// Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	targetElements int,
) (bool, error) {
	if bloomFilter.bloom.Count() < bloomFilter.maxCount {
		return false, nil
	}

	numHashes, numEntries := bloom.OptimalParameters(
		safemath.Max(bloomFilter.minTargetElements, targetElements),
		bloomFilter.falsePositiveProbability,
	)
	newBloom, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return false, err
	}
	salt, err := randomSalt()
	if err != nil {
		return false, err
	}
	bloomFilter.maxCount = bloom.EstimateCount(numHashes, numEntries, bloomFilter.falsePositiveProbability)
	bloomFilter.bloom = newBloom
	bloomFilter.salt = salt
	return true, nil
}

func randomSalt() (ids.ID, error) {
	salt := ids.ID{}
	_, err := rand.Read(salt[:])
	return salt, err
}
