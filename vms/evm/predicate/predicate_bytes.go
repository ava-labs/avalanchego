// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
)

// EndByte is used as a delimiter for the bytes packed into a precompile predicate.
// Precompile predicates are encoded in the Access List of transactions in the access tuples
// which means that its length must be a multiple of 32 (common.HashLength).
// For messages with a length that does not comply to that, this delimiter is used to
// append/remove padding.
const EndByte = 0xff

var (
	ErrInvalidAllZeroBytes = errors.New("predicate specified invalid all zero bytes")
	ErrInvalidPadding      = errors.New("predicate specified invalid padding")
	ErrInvalidEndDelimiter = errors.New("invalid end delimiter")
)

// PackPredicate packs [predicate] by delimiting the actual message with [PredicateEndByte]
// and zero padding to reach a length that is a multiple of 32.
func Pack(bytes []byte) []byte {
	predicateBytes = append(predicateBytes, EndByte)
	return common.RightPadBytes(predicateBytes, (len(predicateBytes)+31)/32*32)
}

// UnpackPredicate unpacks a predicate by stripping right padded zeroes, checking for the delimiter,
// ensuring there is not excess padding, and returning the original message.
// Returns an error if it finds an incorrect encoding.
func UnpackPredicate(paddedPredicate []byte) ([]byte, error) {
	trimmedPredicateBytes := common.TrimRightZeroes(paddedPredicate)
	if len(trimmedPredicateBytes) == 0 {
		return nil, fmt.Errorf("%w: 0x%x", ErrInvalidAllZeroBytes, paddedPredicate)
	}

	if expectedPaddedLength := (len(trimmedPredicateBytes) + 31) / 32 * 32; expectedPaddedLength != len(paddedPredicate) {
		return nil, fmt.Errorf("%w: got length (%d), expected length (%d)", ErrInvalidPadding, len(paddedPredicate), expectedPaddedLength)
	}

	if trimmedPredicateBytes[len(trimmedPredicateBytes)-1] != EndByte {
		return nil, ErrInvalidEndDelimiter
	}

	return trimmedPredicateBytes[:len(trimmedPredicateBytes)-1], nil
}
