// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"fmt"

	"github.com/ava-labs/subnet-evm/params"
	"github.com/ethereum/go-ethereum/common"
)

// PredicateEndByte is used as a delimiter for the bytes packed into a precompile predicate.
// Precompile predicates are encoded in the Access List of transactions in the access tuples
// which means that its length must be a multiple of 32 (common.HashLength).
// For messages with a length that does not comply to that, this delimiter is used to
// append/remove padding.
var PredicateEndByte = byte(0xff)

var (
	ErrInvalidAllZeroBytes = fmt.Errorf("predicate specified invalid all zero bytes")
	ErrInvalidPadding      = fmt.Errorf("predicate specified invalid padding")
	ErrInvalidEndDelimiter = fmt.Errorf("invalid end delimiter")
	ErrorInvalidExtraData  = fmt.Errorf("header extra data too short for predicate verification")
)

// PackPredicate packs [predicate] by delimiting the actual message with [PredicateEndByte]
// and zero padding to reach a length that is a multiple of 32.
func PackPredicate(predicate []byte) []byte {
	predicate = append(predicate, PredicateEndByte)
	return common.RightPadBytes(predicate, (len(predicate)+31)/32*32)
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

	if trimmedPredicateBytes[len(trimmedPredicateBytes)-1] != PredicateEndByte {
		return nil, ErrInvalidEndDelimiter
	}

	return trimmedPredicateBytes[:len(trimmedPredicateBytes)-1], nil
}

// HashSliceToBytes serializes a []common.Hash into a tightly packed byte array.
func HashSliceToBytes(hashes []common.Hash) []byte {
	bytes := make([]byte, common.HashLength*len(hashes))
	for i, hash := range hashes {
		copy(bytes[i*common.HashLength:], hash[:])
	}
	return bytes
}

// BytesToHashSlice packs [b] into a slice of hash values with zero padding
// to the right if the length of b is not a multiple of 32.
func BytesToHashSlice(b []byte) []common.Hash {
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

// GetPredicateResultBytes returns the predicate result bytes from the extra data.
// If the extra data does not contain predicate result bytes, an error is returned.
func GetPredicateResultBytes(extraData []byte) ([]byte, error) {
	// Prior to the DUpgrade, the VM enforces the extra data is smaller than or equal to this size.
	// After the DUpgrade, the VM pre-verifies the extra data past the dynamic fee rollup window is
	// valid.
	if len(extraData) < params.DynamicFeeExtraDataSize {
		return nil, fmt.Errorf("%w: got: %d, required: %d", ErrorInvalidExtraData, len(extraData), params.DynamicFeeExtraDataSize)
	}
	return extraData[params.DynamicFeeExtraDataSize:], nil
}
