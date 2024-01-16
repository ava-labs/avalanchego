// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"crypto/rand"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/math"
)

type bloomFilterMetrics struct {
	resetCount prometheus.Counter
}

// newBloomFilterMetrics returns a common set of metrics
func newBloomFilterMetrics(
	registerer prometheus.Registerer,
	namespace string,
) (bloomFilterMetrics, error) {
	m := bloomFilterMetrics{
		resetCount: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "reset_count",
			Help:      "reset count of bloom filter (n)",
		}),
	}
	err := utils.Err(
		registerer.Register(m.resetCount),
	)
	return m, err
}

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
	numHashes, numEntries := bloom.OptimalParameters(
		minTargetElements,
		targetFalsePositiveProbability,
	)
	b, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return nil, err
	}

	metrics, err := newBloomFilterMetrics(registerer, namespace)
	if err != nil {
		return nil, err
	}

	salt, err := randomSalt()
	return &BloomFilter{
		minTargetElements:              minTargetElements,
		targetFalsePositiveProbability: targetFalsePositiveProbability,
		resetFalsePositiveProbability:  resetFalsePositiveProbability,

		metrics: metrics,

		maxCount: bloom.EstimateCount(numHashes, numEntries, resetFalsePositiveProbability),
		bloom:    b,
		salt:     salt,
	}, err
}

type BloomFilter struct {
	minTargetElements              int
	targetFalsePositiveProbability float64
	resetFalsePositiveProbability  float64

	metrics bloomFilterMetrics

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

	numHashes, numEntries := bloom.OptimalParameters(
		math.Max(bloomFilter.minTargetElements, targetElements),
		bloomFilter.targetFalsePositiveProbability,
	)
	newBloom, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return false, err
	}
	salt, err := randomSalt()
	if err != nil {
		return false, err
	}
	bloomFilter.maxCount = bloom.EstimateCount(numHashes, numEntries, bloomFilter.resetFalsePositiveProbability)
	bloomFilter.bloom = newBloom
	bloomFilter.salt = salt

	bloomFilter.metrics.resetCount.Inc()
	return true, nil
}

func randomSalt() (ids.ID, error) {
	salt := ids.ID{}
	_, err := rand.Read(salt[:])
	return salt, err
}
