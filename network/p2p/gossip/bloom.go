// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"crypto/rand"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
)

// NewBloomFilter returns a new instance of a bloom filter with at least [minTargetElements] elements
// anticipated at any moment, and a false positive probability of [targetFalsePositiveProbability]. If the
// false positive probability exceeds [resetFalsePositiveProbability], the bloom filter will be reset.
//
// The returned bloom filter is safe for concurrent usage.
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
	// A lock is unnecessary as no other goroutine could have access.
	err = filter.resetWhenLocked(minTargetElements)
	return filter, err
}

type BloomFilter struct {
	minTargetElements              int
	targetFalsePositiveProbability float64
	resetFalsePositiveProbability  float64

	metrics *bloom.Metrics

	// [bloom.Filter] itself is threadsafe, but resetting requires replacing it
	// entirely. This mutex protects the [BloomFilter] fields, not the
	// [bloom.Filter], so resetting is a write while everything else is a read.
	resetMu sync.RWMutex

	maxCount int
	bloom    *bloom.Filter
	// salt is provided to eventually unblock collisions in Bloom. It's possible
	// that conflicting Gossipable items collide in the bloom filter, so a salt
	// is generated to eventually resolve collisions.
	salt ids.ID
}

func (b *BloomFilter) Add(gossipable Gossipable) {
	b.resetMu.RLock()
	defer b.resetMu.RUnlock()

	h := gossipable.GossipID()
	bloom.Add(b.bloom, h[:], b.salt[:])
	b.metrics.Count.Inc()
}

func (b *BloomFilter) Has(gossipable Gossipable) bool {
	b.resetMu.RLock()
	defer b.resetMu.RUnlock()

	h := gossipable.GossipID()
	return bloom.Contains(b.bloom, h[:], b.salt[:])
}

func (b *BloomFilter) Marshal() ([]byte, []byte) {
	b.resetMu.RLock()
	defer b.resetMu.RUnlock()

	bloomBytes := b.bloom.Marshal()
	// salt must be copied here to ensure the bytes aren't overwritten if salt
	// is later modified.
	salt := b.salt
	return bloomBytes, salt[:]
}

// ResetBloomFilterIfNeeded resets a bloom filter if it breaches [targetFalsePositiveProbability].
//
// Deprecated: use [BloomFilter.ResetIfNeeded].
func ResetBloomFilterIfNeeded(
	bloomFilter *BloomFilter,
	targetElements int,
) (bool, error) {
	return bloomFilter.ResetIfNeeded(targetElements, nil)
}

// ResetIfNeeded resets the bloom filter if it breaches [targetFalsePositiveProbability].
//
// If [targetElements] exceeds [minTargetElements], the size of the bloom filter will grow to maintain
// the same [targetFalsePositiveProbability].
//
// Returns true if the bloom filter was reset, in which case the `afterReset`
// function is also called (if non-nil) while still holding a mutex excluding
// all other access. This callback is typically used to refill the Bloom filter
// with known elements.
func (b *BloomFilter) ResetIfNeeded(targetElements int, afterReset func() error) (bool, error) {
	mu := &b.resetMu

	// Although this pattern requires a double checking of the same property,
	// it's cheap and avoids unnecessarily locking out all other goroutines on
	// every call to this method.
	isResetNeeded := func() bool {
		return b.bloom.Count() > b.maxCount
	}
	mu.RLock()
	reset := isResetNeeded()
	mu.RUnlock()
	if !reset {
		return false, nil
	}

	mu.Lock()
	defer mu.Unlock()
	// Another thread may have beaten us to acquire the write lock.
	if !isResetNeeded() {
		return false, nil
	}

	targetElements = max(b.minTargetElements, targetElements)
	if err := b.resetWhenLocked(targetElements); err != nil {
		return false, err
	}
	if afterReset == nil {
		return true, nil
	}
	return true, afterReset()
}

func (b *BloomFilter) resetWhenLocked(targetElements int) error {
	numHashes, numEntries := bloom.OptimalParameters(
		targetElements,
		b.targetFalsePositiveProbability,
	)
	newBloom, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return err
	}
	var newSalt ids.ID
	if _, err := rand.Read(newSalt[:]); err != nil {
		return err
	}

	b.maxCount = bloom.EstimateCount(numHashes, numEntries, b.resetFalsePositiveProbability)
	b.bloom = newBloom
	b.salt = newSalt

	b.metrics.Reset(newBloom, b.maxCount)
	return nil
}
