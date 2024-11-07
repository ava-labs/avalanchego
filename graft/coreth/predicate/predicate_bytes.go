// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
)

// DynamicFeeExtraDataSize is defined in the predicate package to avoid a circular dependency.
// After Durango, the extra data past the dynamic fee rollup window represents predicate results.
const DynamicFeeExtraDataSize = 80

// EndByte is used as a delimiter for the bytes packed into a precompile predicate.
// Precompile predicates are encoded in the Access List of transactions in the access tuples
// which means that its length must be a multiple of 32 (common.HashLength).
// For messages with a length that does not comply to that, this delimiter is used to
// append/remove padding.
var EndByte = byte(0xff)

var (
	ErrInvalidAllZeroBytes = fmt.Errorf("predicate specified invalid all zero bytes")
	ErrInvalidPadding      = fmt.Errorf("predicate specified invalid padding")
	ErrInvalidEndDelimiter = fmt.Errorf("invalid end delimiter")
	ErrorInvalidExtraData  = fmt.Errorf("header extra data too short for predicate verification")
)

// PackPredicate packs [predicate] by delimiting the actual message with [PredicateEndByte]
// and zero padding to reach a length that is a multiple of 32.
func PackPredicate(predicateBytes []byte) []byte {
	predicateBytes = append(predicateBytes, EndByte)
	return common.RightPadBytes(predicateBytes, (len(predicateBytes)+31)/32*32)
}

// UnpackPredicate unpacks a predicate by stripping right padded zeroes, checking for the delimter,
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

// GetPredicateResultBytes returns the predicate result bytes from extraData. If
// extraData is too short to include predicate results, it returns nil.
func GetPredicateResultBytes(extraData []byte) []byte {
	// Prior to Durango, the VM enforces the extra data is smaller than or equal
	// to this size.
	if len(extraData) <= DynamicFeeExtraDataSize {
		return nil
	}
	// After Durango, the extra data past the dynamic fee rollup window represents
	// predicate results.
	return extraData[DynamicFeeExtraDataSize:]
}

// SetPredicateResultBytes sets the predicate results in the extraData in the
// block header. This is used to set the predicate results in a block header
// without modifying the initial portion of the extra data (dynamic fee window
// rollup).
func SetPredicateResultBytes(extraData []byte, predicateResults []byte) []byte {
	return append(extraData[:DynamicFeeExtraDataSize], predicateResults...)
}
