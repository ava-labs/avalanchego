// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"crypto/rand"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
)

// NewBloomFilter returns a new instance of a bloom filter with at least [minTargetElements] elements
// anticipated at any moment, and a false positive probability of [targetFalsePositiveProbability]. If the
// false positive probability exceeds [resetFalsePositiveProbability], the bloom filter will be reset.
//
// Invariant: The returned bloom filter is not safe to reset concurrently with
// other operations. However, it is otherwise safe to access concurrently.
func NewBloomFilter(
	registerer prometheus.Registerer,
	namespace string,
	minTargetElements int,
	targetFalsePositiveProbability,
	resetFalsePositiveProbability float64,
) (*BloomFilter, error) {
	metrics, err := bloom.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	filter := &BloomFilter{
		minTargetElements:              minTargetElements,
		targetFalsePositiveProbability: targetFalsePositiveProbability,
		resetFalsePositiveProbability:  resetFalsePositiveProbability,

		metrics: metrics,
	}
	err = resetBloomFilter(
		filter,
		minTargetElements,
		targetFalsePositiveProbability,
		resetFalsePositiveProbability,
	)
	return filter, err
}

type BloomFilter struct {
	minTargetElements              int
	targetFalsePositiveProbability float64
	resetFalsePositiveProbability  float64

	metrics *bloom.Metrics

	maxCount int
	bloom    *bloom.Filter
	// salt is provided to eventually unblock collisions in Bloom. It's possible
	// that conflicting Gossipable items collide in the bloom filter, so a salt
	// is generated to eventually resolve collisions.
	salt ids.ID
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	h := gossipable.GossipID()
	if bloom.Add(b.bloom, h[:], b.salt[:]) {
		b.metrics.Count.Inc()
	}
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

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches [targetFalsePositiveProbability].
//
// If [targetElements] exceeds [minTargetElements], the size of the bloom filter will grow to maintain
// the same [targetFalsePositiveProbability].
//
// Returns true if the bloom filter was reset.
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	targetElements int,
) (bool, error) {
	if bloomFilter.bloom.Count() <= bloomFilter.maxCount {
		return false, nil
	}

	targetElements = max(bloomFilter.minTargetElements, targetElements)
	err := resetBloomFilter(
		bloomFilter,
		targetElements,
		bloomFilter.targetFalsePositiveProbability,
		bloomFilter.resetFalsePositiveProbability,
	)
	return err == nil, err
}

func resetBloomFilter(
	bloomFilter *BloomFilter,
	targetElements int,
	targetFalsePositiveProbability,
	resetFalsePositiveProbability float64,
) error {
	numHashes, numEntries := bloom.OptimalParameters(
		targetElements,
		targetFalsePositiveProbability,
	)
	newBloom, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return err
	}
	var newSalt ids.ID
	if _, err := rand.Read(newSalt[:]); err != nil {
		return err
	}

	bloomFilter.maxCount = bloom.EstimateCount(numHashes, numEntries, resetFalsePositiveProbability)
	bloomFilter.bloom = newBloom
	bloomFilter.salt = newSalt

	bloomFilter.metrics.Reset(newBloom, bloomFilter.maxCount)
	return nil
}
