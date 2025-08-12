// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
)

// Predicate represents a packed predicate that can be stored in EVM access lists.
// It contains the original predicate bytes with padding and delimiter for storage.
type Predicate []byte

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

// New returns a Predicate from b by appending a delimiter and zero-padding
// until the length is a multiple of 32.
func New(predicateBytes []byte) Predicate {
	bytes := make([]byte, len(predicateBytes)+1)
	copy(bytes, predicateBytes)
	bytes[len(predicateBytes)] = delimiter
	return Predicate(common.RightPadBytes(bytes, roundUpTo32(len(bytes))))
}

// Unpack unpacks a predicate by stripping right padded zeroes, checking for the delimiter,
// ensuring there is not excess padding, and returning the original message.
// Returns an error if it finds an incorrect encoding.
func Unpack(predicate Predicate) ([]byte, error) {
	if len(predicate) == 0 {
		return nil, errEmptyPredicate
	}
	trimmedBytes := common.TrimRightZeroes(predicate)
	if len(trimmedBytes) == 0 {
		return nil, fmt.Errorf("%w: 0x%x", errAllZeroBytes, predicate)
	}

	if expectedPaddedLength := roundUpTo32(len(trimmedBytes)); expectedPaddedLength != len(predicate) {
		return nil, fmt.Errorf("%w: got length (%d), expected length (%d)", errExcessPadding, len(predicate), expectedPaddedLength)
	}

	if trimmedBytes[len(trimmedBytes)-1] != delimiter {
		return nil, errWrongEndDelimiter
	}

	return trimmedBytes[:len(trimmedBytes)-1], nil
}
