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

// FromAccessList extracts predicates from a transaction's access list.
// If an address is specified multiple times in the access list, each set of storage
// keys for that address is appended to a slice. Each entry represents a predicate
// encoded as a slice of common.Hash values (32-byte chunks) exactly as stored in
// the access list.
func FromAccessList(rules Predicates, list types.AccessList) map[common.Address][][]common.Hash {
	predicateStorageSlots := make(map[common.Address][][]common.Hash)
	for _, el := range list {
		if !rules.HasPredicate(el.Address) {
			continue
		}
		// Append a copy of the storage keys to avoid aliasing
		keys := make([]common.Hash, len(el.StorageKeys))
		copy(keys, el.StorageKeys)
		predicateStorageSlots[el.Address] = append(predicateStorageSlots[el.Address], keys)
	}

	return predicateStorageSlots
}

func RoundUpTo32(x int) int {
	return (x + 31) / 32 * 32
}

// pack returns a 32-byte aligned predicate from b by appending a delimiter and zero-padding
// until the length is a multiple of 32. 32-byte aligned predicates should not be exposed
// outside of this package.
func pack(b []byte) []byte {
	bytes := make([]byte, len(b)+1)
	copy(bytes, b)
	bytes[len(b)] = delimiter

	return common.RightPadBytes(bytes, RoundUpTo32(len(bytes)))
}

// Unpack unpacks a predicate by stripping right padded zeroes, checking for the delimiter,
// ensuring there is not excess padding, and returning the original message.
// Returns an error if it finds an incorrect encoding.
func Unpack(p []byte) ([]byte, error) {
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
