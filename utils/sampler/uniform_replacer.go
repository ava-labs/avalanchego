// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sampler

type defaultMap map[uint64]uint64

func (m defaultMap) get(key uint64, defaultVal uint64) uint64 {
	if val, ok := m[key]; ok {
		return val
	}
	return defaultVal
}

// uniformReplacer allows for sampling over a uniform distribution without
// replacement.
//
// Sampling is performed by lazily performing an array shuffle of the array
// [0, 1, ..., length - 1]. By performing the first count swaps of this shuffle,
// we can create an array of length count with elements sampled with uniform
// probability.
//
// Initialization takes O(1) time.
//
// Sampling is performed in O(count) time and O(count) space.
type uniformReplacer struct {
	rng        *rng
	length     uint64
	drawn      defaultMap
	drawsCount uint64
}

func (s *uniformReplacer) Initialize(length uint64) {
	s.length = length
	s.drawn = make(defaultMap)
	s.drawsCount = 0
}

func (s *uniformReplacer) Sample(count int) ([]uint64, bool) {
	s.Reset()

	results := make([]uint64, count)
	for i := 0; i < count; i++ {
		ret, hasNext := s.Next()
		if !hasNext {
			return nil, false
		}
		results[i] = ret
	}
	return results, true
}

func (s *uniformReplacer) Reset() {
	clear(s.drawn)
	s.drawsCount = 0
}

func (s *uniformReplacer) Next() (uint64, bool) {
	if s.drawsCount >= s.length {
		return 0, false
	}

	draw := s.rng.Uint64Inclusive(s.length-1-s.drawsCount) + s.drawsCount
	ret := s.drawn.get(draw, draw)
	s.drawn[draw] = s.drawn.get(s.drawsCount, s.drawsCount)
	s.drawsCount++

	return ret, true
}
