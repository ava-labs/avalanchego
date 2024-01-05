// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"errors"

	"github.com/ava-labs/avalanchego/utils/bloom"
)

const bytesPerSeed = 8

var errMaxBytes = errors.New("too large")

type Filter interface {
	// Add adds to filter, assumed thread safe
	Add(...[]byte)

	// Check checks filter, assumed thread safe
	Check([]byte) bool
}

func New(maxN int, p float64, maxBytes int) (Filter, error) {
	numSeeds, numBytes := bloom.OptimalParameters(maxN, p)
	if neededBytes := 1 + numSeeds*bytesPerSeed + numBytes; neededBytes > maxBytes {
		return nil, errMaxBytes
	}
	f, err := bloom.New(numSeeds, numBytes)
	return &fitler{
		filter: f,
	}, err
}

type fitler struct {
	filter *bloom.Filter
}

func (f *fitler) Add(bl ...[]byte) {
	for _, b := range bl {
		bloom.Add(f.filter, b, nil)
	}
}

func (f *fitler) Check(b []byte) bool {
	return bloom.Contains(f.filter, b, nil)
}
