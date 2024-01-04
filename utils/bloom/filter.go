// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"sync"
)

const (
	minSeeds   = 1
	maxSeeds   = 16 // Supports a false positive probability of 2^-16 when using optimal size values
	minEntries = 1

	bitsPerByte    = 8
	bytesPerUint64 = 8
	hashRotation   = 17
)

var (
	errInvalidNumSeeds = errors.New("invalid num seeds")
	errTooFewSeeds     = errors.New("too few seeds")
	errTooManySeeds    = errors.New("too many seeds")
	errPaddedNumSeeds  = errors.New("number of seeds unnecessarily padded")
	errTooFewEntries   = errors.New("too few entries")
)

type Filter struct {
	numBits uint64

	lock    sync.RWMutex
	seeds   []uint64
	entries []byte
	count   int
}

// New creates a new Filter with the specified number of seeds and bytes for
// entries.
func New(numSeeds, numBytes int) (*Filter, error) {
	if numBytes < minEntries {
		return nil, errTooFewEntries
	}

	seeds, err := newSeeds(numSeeds)
	if err != nil {
		return nil, err
	}

	return &Filter{
		numBits: uint64(numBytes * bitsPerByte),
		seeds:   seeds,
		entries: make([]byte, numBytes),
		count:   0,
	}, nil
}

// Parameters returns the [numSeeds] and [numBytes] that were used when creating
// this filter.
func (f *Filter) Parameters() (int, int) {
	return len(f.seeds), len(f.entries)
}

func (f *Filter) Add(hash uint64) {
	f.lock.Lock()
	defer f.lock.Unlock()

	for _, seed := range f.seeds {
		hash = bits.RotateLeft64(hash, hashRotation) ^ seed
		index := hash % f.numBits
		byteIndex := index / bitsPerByte
		bitIndex := index % bitsPerByte
		f.entries[byteIndex] |= 1 << bitIndex
	}
	f.count++
}

// FalsePositiveProbability is a lower-bound on the probability of false
// positives. For values where numBits >> numSeeds, the predicted probability is
// fairly accurate.
func (f *Filter) FalsePositiveProbability() float64 {
	numSeeds := float64(len(f.seeds))
	numBits := float64(f.numBits)

	f.lock.RLock()
	numAdded := float64(f.count)
	f.lock.RUnlock()

	return falsePositiveProbability(numSeeds, numBits, numAdded)
}

func (f *Filter) Contains(hash uint64) bool {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return contains(f.seeds, f.entries, hash)
}

func (f *Filter) Marshal() []byte {
	f.lock.RLock()
	defer f.lock.RUnlock()

	return marshal(f.seeds, f.entries)
}

func newSeeds(numSeeds int) ([]uint64, error) {
	switch {
	case numSeeds < minSeeds:
		return nil, fmt.Errorf("%w: %d < %d", errTooFewSeeds, numSeeds, minSeeds)
	case numSeeds > maxSeeds:
		return nil, fmt.Errorf("%w: %d > %d", errTooManySeeds, numSeeds, maxSeeds)
	}

	seedsBytes := make([]byte, numSeeds*bytesPerUint64)
	if _, err := rand.Reader.Read(seedsBytes); err != nil {
		return nil, err
	}

	seeds := make([]uint64, numSeeds)
	for i := range seeds {
		seeds[i] = binary.BigEndian.Uint64(seedsBytes[i*bytesPerUint64:])
	}
	return seeds, nil
}

// ref: https://tsapps.nist.gov/publication/get_pdf.cfm?pub_id=903775
func falsePositiveProbability(numSeeds, numBits, numAdded float64) float64 {
	bitCollisionProbability := 1. - math.Exp(-numSeeds*numAdded/numBits)
	return math.Pow(bitCollisionProbability, numSeeds)
}

func contains(seeds []uint64, entries []byte, hash uint64) bool {
	var (
		numBits          = bitsPerByte * uint64(len(entries))
		accumulator byte = 1
	)
	for seedIndex := 0; seedIndex < len(seeds) && accumulator != 0; seedIndex++ {
		hash = bits.RotateLeft64(hash, hashRotation) ^ seeds[seedIndex]
		index := hash % numBits
		byteIndex := index / bitsPerByte
		bitIndex := index % bitsPerByte
		accumulator &= entries[byteIndex] >> bitIndex
	}
	return accumulator != 0
}

func marshal(seeds []uint64, entries []byte) []byte {
	numSeeds := len(seeds)
	numSeedsUint64 := uint64(numSeeds)
	seedsOffset := uintSize(numSeedsUint64)
	entriesOffset := seedsOffset + numSeeds*bytesPerUint64

	bytes := make([]byte, entriesOffset+len(entries))
	binary.PutUvarint(bytes, numSeedsUint64)
	for i, seed := range seeds {
		binary.BigEndian.PutUint64(bytes[seedsOffset+i*bytesPerUint64:], seed)
	}
	copy(bytes[entriesOffset:], entries)
	return bytes
}

func uintSize(value uint64) int {
	if value == 0 {
		return 1
	}
	return (bits.Len64(value) + 6) / 7
}
