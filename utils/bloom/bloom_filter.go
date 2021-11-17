// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"errors"
	"sync"

	"github.com/spaolacci/murmur3"

	streakKnife "github.com/holiman/bloomfilter/v2"
)

var errMaxBytes = errors.New("too large")

type Filter interface {
	// Add adds to filter, assumed thread safe
	Add(...[]byte)

	// Check checks filter, assumed thread safe
	Check([]byte) bool
}

func New(maxN uint64, p float64, maxBytes uint64) (Filter, error) {
	neededBytes := bytesSteakKnifeFilter(maxN, p)
	if neededBytes > maxBytes {
		return nil, errMaxBytes
	}
	return newSteakKnifeFilter(maxN, p)
}

type steakKnifeFilter struct {
	lock   sync.RWMutex
	filter *streakKnife.Filter
}

func bytesSteakKnifeFilter(maxN uint64, p float64) uint64 {
	m := streakKnife.OptimalM(maxN, p)
	k := streakKnife.OptimalK(m, maxN)

	// This is pulled from bloomFilter.newBits and bloomfilter.newRandKeys. The
	// calculation is the size of the bitset which would be created from this
	// filter.
	mSize := (m + 63) / 64
	totalSize := mSize + k

	return totalSize * 8 // 8 == sizeof(uint64))
}

func newSteakKnifeFilter(maxN uint64, p float64) (Filter, error) {
	m := streakKnife.OptimalM(maxN, p)
	k := streakKnife.OptimalK(m, maxN)

	filter, err := streakKnife.New(m, k)
	return &steakKnifeFilter{filter: filter}, err
}

func (f *steakKnifeFilter) Add(bl ...[]byte) {
	f.lock.Lock()
	defer f.lock.Unlock()

	for _, b := range bl {
		h := murmur3.New64()
		_, _ = h.Write(b)
		f.filter.Add(h)
	}
}

func (f *steakKnifeFilter) Check(b []byte) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()

	h := murmur3.New64()
	_, _ = h.Write(b)
	return f.filter.Contains(h)
}
