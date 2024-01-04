// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"crypto/rand"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
)

// NewBloomFilter returns a new instance of a bloom filter with at most
// [maxExpectedElements] elements anticipated at any moment, and a false
// positive probability of [falsePositiveProbability].
func NewBloomFilter(
	maxExpectedElements uint64,
	falsePositiveProbability float64,
) (*BloomFilter, error) {
	bloom, err := bloom.New(bloom.OptimalParameters(
		maxExpectedElements,
		falsePositiveProbability,
	))
	if err != nil {
		return nil, err
	}

	salt, err := randomSalt()
	return &BloomFilter{
		bloom: bloom,
		salt:  salt,
	}, err
}

type BloomFilter struct {
	bloom *bloom.Filter
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

// TODO: Remove error from the return
func (b *BloomFilter) Marshal() ([]byte, []byte, error) {
	bloomBytes := b.bloom.Marshal()
	// salt must be copied here to ensure the bytes aren't overwritten if salt
	// is later modified.
	salt := b.salt
	return bloomBytes, salt[:], nil
}

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches a target false
// positive probability. Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	falsePositiveProbability float64,
) (bool, error) {
	if bloomFilter.bloom.FalsePositiveProbability() < falsePositiveProbability {
		return false, nil
	}

	newBloom, err := bloom.New(bloomFilter.bloom.Parameters())
	if err != nil {
		return false, err
	}
	salt, err := randomSalt()
	if err != nil {
		return false, err
	}

	bloomFilter.bloom = newBloom
	bloomFilter.salt = salt
	return true, nil
}

func randomSalt() (ids.ID, error) {
	salt := ids.ID{}
	_, err := rand.Read(salt[:])
	return salt, err
}
