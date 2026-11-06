// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

// delimiter separates the actual predicate bytes from the padded zero bytes.
//
// Predicates are encoded in the Access List of transactions by using in the
// access tuples. This means that the length must be a multiple of
// [common.HashLength].
//
// Even if the original predicate bytes is a multiple of [common.HashLength],
// the delimiter must be appended to support decoding.
const delimiter = 0xff

var (
	errMissingDelimiter = errors.New("no delimiter found")
	errExcessPadding    = errors.New("predicate included excess padding")
	errWrongDelimiter   = errors.New("wrong delimiter")
)

// Predicate is a message padded with the delimiter and zeros and chunked into
// 32-byte chunks.
type Predicate []common.Hash

// New constructs a predicate from raw predicate bytes.
//
// It chunks the predicate by appending [predicate.Delimiter] and zero-padding
// to a multiple of 32 bytes.
func New(b []byte) Predicate {
	numUnpaddedChunks := len(b) / common.HashLength
	chunks := make([]common.Hash, numUnpaddedChunks+1)
	// Copy over chunks that don't require padding.
	for i := range chunks[:numUnpaddedChunks] {
		chunks[i] = common.Hash(b[common.HashLength*i:])
	}

	// Add the delimiter and required padding to the last chunk.
	copy(chunks[numUnpaddedChunks][:], b[common.HashLength*numUnpaddedChunks:])
	chunks[numUnpaddedChunks][len(b)%common.HashLength] = delimiter
	return chunks
}

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
		return nil, fmt.Errorf("%w: length (%d)", errMissingDelimiter, len(p))
	}

	expectedLen := (len(trimmed) + common.HashLength - 1) / common.HashLength
	if expectedLen != len(p) {
		return nil, fmt.Errorf("%w: got length (%d), expected length (%d)", errExcessPadding, len(p), expectedLen)
	}

	delimiterIndex := len(trimmed) - 1
	if trimmed[delimiterIndex] != delimiter {
		return nil, errWrongDelimiter
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
