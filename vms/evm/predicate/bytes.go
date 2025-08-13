// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
)

// delimiter is used as a delimiter for the bytes packed into a precompile predicate.
// Precompile predicates are encoded in the Access List of transactions in the access tuples
// which means that its length must be a multiple of 32 (common.HashLength).
// While the delimiter is always included, for messages that are not a multiple of 32,
// this delimiter is used to append/remove padding.
const delimiter = 0xff

var (
	errEmptyPredicate    = errors.New("predicate specified empty predicate")
	errAllZeroBytes      = errors.New("predicate specified all zero bytes")
	errExcessPadding     = errors.New("predicate specified excess padding")
	errWrongEndDelimiter = errors.New("wrong end delimiter")
)

// bytesToHashSlice packs [b] into a slice of hash values with zero padding
// to the right if the length of b is not a multiple of 32.
func bytesToHashSlice(b []byte) []common.Hash {
	var (
		numHashes = roundUpTo32(len(b)) / common.HashLength
		hashes    = make([]common.Hash, numHashes)
	)

	for i := range hashes {
		start := i * common.HashLength
		copy(hashes[i][:], b[start:])
	}

	return hashes
}

// hashSliceToBytes serializes a []common.Hash into a tightly packed byte array.
func hashSliceToBytes(hashes []common.Hash) []byte {
	bytes := make([]byte, common.HashLength*len(hashes))
	for i, hash := range hashes {
		copy(bytes[i*common.HashLength:], hash[:])
	}

	return bytes
}

func roundUpTo32(x int) int {
	return (x + 31) / 32 * 32
}

// Predicate represents a packed predicate that can be stored in EVM access lists.
// It contains the original predicate bytes with padding and delimiter for storage.
type Predicate []byte

// New returns a Predicate from b by appending a delimiter and zero-padding
// until the length is a multiple of 32.
func New(b []byte) Predicate {
	bytes := make([]byte, len(b)+1)
	copy(bytes, b)
	bytes[len(b)] = delimiter
	return Predicate(common.RightPadBytes(bytes, roundUpTo32(len(bytes))))
}

// Unpack unpacks a predicate by stripping right padded zeroes, checking for the delimiter,
// ensuring there is not excess padding, and returning the original message.
// Returns an error if it finds an incorrect encoding.
func Unpack(p Predicate) ([]byte, error) {
	if len(p) == 0 {
		return nil, errEmptyPredicate
	}
	trim := common.TrimRightZeroes(p)
	if len(trim) == 0 {
		return nil, fmt.Errorf("%w: length (%d)", errAllZeroBytes, len(p))
	}

	if paddedLength := roundUpTo32(len(trim)); paddedLength != len(p) {
		return nil, fmt.Errorf("%w: got length (%d), expected length (%d)", errExcessPadding, len(p), paddedLength)
	}

	if trim[len(trim)-1] != delimiter {
		return nil, errWrongEndDelimiter
	}

	return trim[:len(trim)-1], nil
}
