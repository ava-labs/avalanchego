// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"crypto/rand"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	_ Set[Gossipable]             = (*SetWithBloomFilter[Gossipable])(nil)
	_ PullGossiperSet[Gossipable] = (*SetWithBloomFilter[Gossipable])(nil)
)

type Set[T Gossipable] interface {
	HandlerSet[T]
	PushGossiperSet
	// Len returns the number of items in the set.
	//
	// This value should always be at least as large as the number of items that
	// can be iterated over with a call to Iterate.
	Len() int
}

// NewSetWithBloomFilter wraps set with a bloom filter. It is expected for all
// future additions to the provided set to go through the returned set, or be
// followed by a call to AddToBloom.
func NewSetWithBloomFilter[T Gossipable](
	set Set[T],
	registerer prometheus.Registerer,
	namespace string,
	minTargetElements int,
	targetFalsePositiveProbability,
	resetFalsePositiveProbability float64,
) (*SetWithBloomFilter[T], error) {
	metrics, err := bloom.NewMetrics(namespace, registerer)
	if err != nil {
		return nil, err
	}
	m := &SetWithBloomFilter[T]{
		set: set,

		minTargetElements:              minTargetElements,
		targetFalsePositiveProbability: targetFalsePositiveProbability,
		resetFalsePositiveProbability:  resetFalsePositiveProbability,

		metrics: metrics,
	}
	return m, m.resetBloomFilter()
}

type SetWithBloomFilter[T Gossipable] struct {
	set Set[T]

	minTargetElements              int
	targetFalsePositiveProbability float64
	resetFalsePositiveProbability  float64

	metrics *bloom.Metrics

	l        sync.RWMutex
	maxCount int
	bloom    *bloom.Filter
	// salt is provided to eventually unblock collisions in Bloom. It's possible
	// that conflicting Gossipable items collide in the bloom filter, so a salt
	// is generated to eventually resolve collisions.
	salt ids.ID
}

func (s *SetWithBloomFilter[T]) Add(v T) error {
	if err := s.set.Add(v); err != nil {
		return err
	}
	return s.addToBloom(v.GossipID())
}

// addToBloom adds the provided ID to the bloom filter.
//
// Even if an error is returned, the ID has still been added to the bloom
// filter.
func (s *SetWithBloomFilter[T]) addToBloom(h ids.ID) error {
	s.l.RLock()
	if bloom.Add(s.bloom, h[:], s.salt[:]) {
		s.metrics.Count.Inc()
	}
	shouldReset := s.shouldReset()
	s.l.RUnlock()

	if !shouldReset {
		return nil
	}

	s.l.Lock()
	defer s.l.Unlock()

	// Bloom filter was already reset by another thread
	if !s.shouldReset() {
		return nil
	}
	return s.resetBloomFilter()
}

func (s *SetWithBloomFilter[T]) shouldReset() bool {
	return s.bloom.Count() > s.maxCount
}

// resetBloomFilter attempts to generate a new bloom filter and fill it with the
// current entries in the set.
//
// If an error is returned, the bloom filter and salt are unchanged.
func (s *SetWithBloomFilter[T]) resetBloomFilter() error {
	targetElements := max(2*s.set.Len(), s.minTargetElements)
	numHashes, numEntries := bloom.OptimalParameters(
		targetElements,
		s.targetFalsePositiveProbability,
	)
	newBloom, err := bloom.New(numHashes, numEntries)
	if err != nil {
		return fmt.Errorf("creating new bloom: %w", err)
	}
	var newSalt ids.ID
	if _, err := rand.Read(newSalt[:]); err != nil {
		return fmt.Errorf("generating new salt: %w", err)
	}
	s.set.Iterate(func(v T) bool {
		h := v.GossipID()
		bloom.Add(newBloom, h[:], newSalt[:])
		return true
	})

	s.maxCount = bloom.EstimateCount(numHashes, numEntries, s.resetFalsePositiveProbability)
	s.bloom = newBloom
	s.salt = newSalt

	s.metrics.Reset(newBloom, s.maxCount)
	return nil
}

func (s *SetWithBloomFilter[T]) Has(h ids.ID) bool {
	return s.set.Has(h)
}

func (s *SetWithBloomFilter[T]) Iterate(f func(T) bool) {
	s.set.Iterate(f)
}

func (s *SetWithBloomFilter[T]) Len() int {
	return s.set.Len()
}

func (s *SetWithBloomFilter[_]) BloomFilter() (*bloom.Filter, ids.ID) {
	s.l.RLock()
	defer s.l.RUnlock()

	return s.bloom, s.salt
}
