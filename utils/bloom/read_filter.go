// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"encoding/binary"
	"fmt"
)

var (
	EmptyFilter = &ReadFilter{
		hashSeeds: make([]uint64, minHashes),
		entries:   make([]byte, minEntries),
	}
	FullFilter = &ReadFilter{
		hashSeeds: make([]uint64, minHashes),
		entries:   make([]byte, minEntries),
	}
)

func init() {
	for i := range FullFilter.entries {
		FullFilter.entries[i] = 0xFF
	}
}

type ReadFilter struct {
	hashSeeds []uint64
	entries   []byte
}

// Parse [bytes] into a read-only bloom filter.
func Parse(bytes []byte) (*ReadFilter, error) {
	if len(bytes) == 0 {
		return nil, errInvalidNumHashes
	}
	numHashes := bytes[0]
	entriesOffset := 1 + int(numHashes)*bytesPerUint64
	switch {
	case numHashes < minHashes:
		return nil, fmt.Errorf("%w: %d < %d", errTooFewHashes, numHashes, minHashes)
	case numHashes > maxHashes:
		return nil, fmt.Errorf("%w: %d > %d", errTooManyHashes, numHashes, maxHashes)
	case len(bytes) < entriesOffset+minEntries: // numEntries = len(bytes) - entriesOffset
		return nil, errTooFewEntries
	}

	f := &ReadFilter{
		hashSeeds: make([]uint64, numHashes),
		entries:   bytes[entriesOffset:],
	}
	for i := range f.hashSeeds {
		f.hashSeeds[i] = binary.BigEndian.Uint64(bytes[1+i*bytesPerUint64:])
	}
	return f, nil
}

func (f *ReadFilter) Contains(hash uint64) bool {
	return contains(f.hashSeeds, f.entries, hash)
}

func (f *ReadFilter) Marshal() []byte {
	return marshal(f.hashSeeds, f.entries)
}
