// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
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

type Predicates interface {
	HasPredicate(address common.Address) bool
}

// FromAccessList extracts predicates from a transaction's access list
// If an address is specified multiple times in the access list, each storage slot for that address is
// appended to a slice of byte slices. Each byte slice represents a predicate, making it a slice of predicates
// for each access list address, and every predicate in the slice goes through verification.
func FromAccessList(rules Predicates, list types.AccessList) map[common.Address][]Predicate {
	predicateStorageSlots := make(map[common.Address][]Predicate)
	for _, el := range list {
		if !rules.HasPredicate(el.Address) {
			continue
		}
		predicateStorageSlots[el.Address] = append(predicateStorageSlots[el.Address], HashSliceToBytes(el.StorageKeys))
	}

	return predicateStorageSlots
}

// hashSliceToBytes serializes a []common.Hash into a tightly packed byte array.
func HashSliceToBytes(hashes []common.Hash) []byte {
	bytes := make([]byte, common.HashLength*len(hashes))
	for i, hash := range hashes {
		copy(bytes[i*common.HashLength:], hash[:])
	}

	return bytes
}

func RoundUpTo32(x int) int {
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

	return Predicate(common.RightPadBytes(bytes, RoundUpTo32(len(bytes))))
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

	if paddedLength := RoundUpTo32(len(trim)); paddedLength != len(p) {
		return nil, fmt.Errorf("%w: got length (%d), expected length (%d)", errExcessPadding, len(p), paddedLength)
	}

	if trim[len(trim)-1] != delimiter {
		return nil, errWrongEndDelimiter
	}

	return trim[:len(trim)-1], nil
}
