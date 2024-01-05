// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"crypto/rand"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const targetSizeMultiplier = 2

// NewBloomFilter returns a new instance of a bloom filter with at least [minExpectedElements] elements
// anticipated at any moment, and a false positive probability of [falsePositiveProbability].
//
// If the number of elements that are tracked with the bloom filter exceeds [minExpectedElements], the size
// of the bloom filter will grow to maintain the same [falsePositiveProbability].
func NewBloomFilter(
	minExpectedElements int,
	falsePositiveProbability float64,
) (*BloomFilter, error) {
	bloom, err := bloom.New(bloom.OptimalParameters(
		minExpectedElements,
		falsePositiveProbability,
	))
	if err != nil {
		return nil, err
	}

	salt, err := randomSalt()
	return &BloomFilter{
		minExpectedElements: minExpectedElements,
		bloom:               bloom,
		salt:                salt,
	}, err
}

type BloomFilter struct {
	l  sync.RWMutex
	rl sync.Mutex

	minExpectedElements int
	bloom               *bloom.Filter
	// salt is provided to eventually unblock collisions in Bloom. It's possible
	// that conflicting Gossipable items collide in the bloom filter, so a salt
	// is generated to eventually resolve collisions.
	salt ids.ID
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	b.l.RLock()
	defer b.l.RUnlock()

	h := gossipable.GossipID()
	bloom.Add(b.bloom, h[:], b.salt[:])
}

func (b *BloomFilter) Has(gossipable Gossipable) bool {
	b.l.RLock()
	defer b.l.RUnlock()

	h := gossipable.GossipID()
	return bloom.Contains(b.bloom, h[:], b.salt[:])
}

func (b *BloomFilter) Marshal() ([]byte, []byte) {
	b.l.RLock()
	defer b.l.RUnlock()

	bloomBytes := b.bloom.Marshal()
	// salt must be copied here to ensure the bytes aren't overwritten if salt
	// is later modified.
	salt := b.salt
	return bloomBytes, salt[:]
}

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches a target false
// positive probability. Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	falsePositiveProbability float64,
	currentSize int,
) (bool, error) {
	bloomFilter.l.RLock()
	numHashes, numEntries := bloomFilter.bloom.Parameters()
	// TODO: Precalculate maxCount, as it is independent of the current state
	// of the bloom filter.
	maxCount := bloom.EstimateCount(numHashes, numEntries, falsePositiveProbability)
	if bloomFilter.bloom.Count() < maxCount {
		bloomFilter.l.RUnlock()
		return false, nil
	}

	bloomFilter.l.RUnlock()
	bloomFilter.l.Lock()
	defer bloomFilter.l.Unlock()
	newBloom, err := bloom.New(bloom.OptimalParameters(
		safemath.Max(bloomFilter.minExpectedElements, currentSize*targetSizeMultiplier),
		falsePositiveProbability,
	))
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
