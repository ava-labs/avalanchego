// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"encoding/binary"
	"fmt"
)

type ReadFilter struct {
	seeds   []uint64
	entries []byte
}

func Parse(bytes []byte) (*ReadFilter, error) {
	numSeeds, seedsOffset := binary.Uvarint(bytes)
	switch {
	case seedsOffset <= 0:
		return nil, errInvalidNumSeeds
	case numSeeds < minSeeds:
		return nil, fmt.Errorf("%w: %d < %d", errTooFewSeeds, numSeeds, minSeeds)
	case numSeeds > maxSeeds:
		return nil, fmt.Errorf("%w: %d > %d", errTooManySeeds, numSeeds, maxSeeds)
	}

	if expectedSeedsOffset := uintSize(numSeeds); expectedSeedsOffset != seedsOffset {
		return nil, fmt.Errorf("%w: %d != %d", errPaddedNumSeeds, seedsOffset, expectedSeedsOffset)
	}

	entriesOffset := seedsOffset + int(numSeeds)*bytesPerUint64
	if len(bytes) < entriesOffset+minEntries { // numEntries = len(bytes) - entriesOffset
		return nil, errTooFewEntries
	}

	f := &ReadFilter{
		seeds:   make([]uint64, numSeeds),
		entries: bytes[entriesOffset:],
	}
	for i := range f.seeds {
		f.seeds[i] = binary.BigEndian.Uint64(bytes[seedsOffset+i*bytesPerUint64:])
	}
	return f, nil
}

func (f *ReadFilter) Contains(hash uint64) bool {
	return contains(f.seeds, f.entries, hash)
}

func (f *ReadFilter) Marshal() []byte {
	return marshal(f.seeds, f.entries)
}
