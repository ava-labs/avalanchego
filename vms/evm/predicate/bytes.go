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
// For messages with a length that does not comply to that, this delimiter is used to
// append/remove padding.
const delimiter = 0xff

var (
	errInvalidEmptyPredicate = errors.New("predicate specified invalid empty predicate")
	errInvalidAllZeroBytes   = errors.New("predicate specified invalid all zero bytes")
	errInvalidPadding        = errors.New("predicate specified invalid padding")
	errInvalidEndDelimiter   = errors.New("invalid end delimiter")
)

// bytesToHashSlice packs [b] into a slice of hash values with zero padding
// to the right if the length of b is not a multiple of 32.
func bytesToHashSlice(b []byte) []common.Hash {
	var (
		numHashes = (len(b) + 31) / 32
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

// New creates a Predicate from predicate bytes by delimiting the actual
// message with delimiter and zero padding to reach a length that is a
// multiple of 32. Returns a Predicate that can be stored in EVM access lists.
func New(predicateBytes []byte) Predicate {
	bytes := append(predicateBytes, delimiter)
	return Predicate(common.RightPadBytes(bytes, (len(bytes)+31)/32*32))
}

// Unpack unpacks a predicate by stripping right padded zeroes, checking for the delimiter,
// ensuring there is not excess padding, and returning the original message.
// Returns an error if it finds an incorrect encoding.
func Unpack(predicate Predicate) ([]byte, error) {
	if len(predicate) == 0 {
		return nil, fmt.Errorf("%w: 0x%x", errInvalidEmptyPredicate, predicate)
	}

	trimmedBytes := common.TrimRightZeroes(predicate)
	if len(trimmedBytes) == 0 {
		return nil, fmt.Errorf("%w: 0x%x", errInvalidAllZeroBytes, predicate)
	}

	if expectedPaddedLength := (len(trimmedBytes) + 31) / 32 * 32; expectedPaddedLength != len(predicate) {
		return nil, fmt.Errorf("%w: got length (%d), expected length (%d)", errInvalidPadding, len(predicate), expectedPaddedLength)
	}

	if trimmedBytes[len(trimmedBytes)-1] != delimiter {
		return nil, errInvalidEndDelimiter
	}

	return trimmedBytes[:len(trimmedBytes)-1], nil
}
