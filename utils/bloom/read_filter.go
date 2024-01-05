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
	if len(bytes) == 0 {
		return nil, errInvalidNumSeeds
	}
	numSeeds := bytes[0]
	entriesOffset := 1 + int(numSeeds)*bytesPerUint64
	switch {
	case numSeeds < minSeeds:
		return nil, fmt.Errorf("%w: %d < %d", errTooFewSeeds, numSeeds, minSeeds)
	case numSeeds > maxSeeds:
		return nil, fmt.Errorf("%w: %d > %d", errTooManySeeds, numSeeds, maxSeeds)
	case len(bytes) < entriesOffset+minEntries: // numEntries = len(bytes) - entriesOffset
		return nil, errTooFewEntries
	}

	f := &ReadFilter{
		seeds:   make([]uint64, numSeeds),
		entries: bytes[entriesOffset:],
	}
	for i := range f.seeds {
		f.seeds[i] = binary.BigEndian.Uint64(bytes[1+i*bytesPerUint64:])
	}
	return f, nil
}

func (f *ReadFilter) Contains(hash uint64) bool {
	return contains(f.seeds, f.entries, hash)
}

func (f *ReadFilter) Marshal() []byte {
	return marshal(f.seeds, f.entries)
}
