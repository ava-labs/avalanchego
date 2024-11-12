// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/bloom"
)

const bytesPerHash = 8

var (
	_ Filter = (*filter)(nil)

	errMaxBytes = errors.New("too large")
)

type Filter interface {
	// Add adds to filter, assumed thread safe
	Add(...[]byte)

	// Check checks filter, assumed thread safe
	Check([]byte) bool
}

func New(maxN int, p float64, maxBytes int) (Filter, error) {
	numHashes, numEntries := bloom.OptimalParameters(maxN, p)
	if neededBytes := 1 + numHashes*bytesPerHash + numEntries; neededBytes > maxBytes {
		return nil, errMaxBytes
	}
	f, err := bloom.New(numHashes, numEntries)
	return &filter{
		filter: f,
	}, err
}

type filter struct {
	filter *bloom.Filter
}

func (f *filter) Add(bl ...[]byte) {
	for _, b := range bl {
		bloom.Add(f.filter, b, nil)
	}
}

func (f *filter) Check(b []byte) bool {
	return bloom.Contains(f.filter, b, nil)
}
