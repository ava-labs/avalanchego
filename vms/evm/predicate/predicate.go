// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

// Delimiter separates the actual predicate bytes from the padded zero bytes.
//
// Predicates are encoded in the Access List of transactions by using in the
// access tuples. This means that the length must be a multiple of
// [common.HashLength].
//
// Even if the original predicate bytes is a multiple of [common.HashLength],
// the delimiter must be appended to support decoding.
const Delimiter = 0xff

var (
	ErrMissingDelimiter  = errors.New("no delimiter found")
	ErrExcessPadding     = errors.New("predicate included excess padding")
	ErrWrongEndDelimiter = errors.New("wrong delimiter")
)

// Predicate is a message padded with the delimiter and zeros and chunked into
// 32-byte chunks.
type Predicate []common.Hash

// Bytes converts the chunked predicate into the original message.
//
// Returns an error if it finds an incorrect encoding.
func (p Predicate) Bytes() ([]byte, error) {
	padded := make([]byte, common.HashLength*len(p))
	for i, chunk := range p {
		copy(padded[common.HashLength*i:], chunk[:])
	}
	trimmed := common.TrimRightZeroes(padded)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("%w: length (%d)", ErrMissingDelimiter, len(p))
	}

	expectedPredicateLen := (len(trimmed) + common.HashLength - 1) / common.HashLength
	if expectedPredicateLen != len(p) {
		return nil, fmt.Errorf("%w: got length (%d), expected length (%d)", ErrExcessPadding, len(p), expectedPredicateLen)
	}

	delimiterIndex := len(trimmed) - 1
	if trimmed[delimiterIndex] != Delimiter {
		return nil, ErrWrongEndDelimiter
	}

	return trimmed[:delimiterIndex], nil
}

type Predicates interface {
	HasPredicate(address common.Address) bool
}

// FromAccessList extracts predicates from a transaction's access list.
//
// If an address is specified multiple times in the access list, each set of
// storage keys for that address is considered an individual predicate.
func FromAccessList(rules Predicates, list types.AccessList) map[common.Address][]Predicate {
	predicates := make(map[common.Address][]Predicate)
	for _, el := range list {
		if !rules.HasPredicate(el.Address) {
			continue
		}
		predicates[el.Address] = append(predicates[el.Address], el.StorageKeys)
	}

	return predicates
}
