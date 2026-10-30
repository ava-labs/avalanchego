// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpmsg

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
)

// delimiter marks the end of the message bytes.
//
// Messages are stored as access-list storage keys, so the packed length must
// be a multiple of [common.HashLength]. The delimiter is appended even when the
// message length already is one, so that decoding works.
const delimiter = 0xff

var (
	errMissingDelimiter = errors.New("no delimiter found")
	errExcessPadding    = errors.New("excess padding")
	errWrongDelimiter   = errors.New("wrong delimiter")
)

// Packed is a message padded with the delimiter and zeros and chunked into
// 32-byte chunks.
type Packed []common.Hash

// Pack packs b into storage keys.
//
// It appends the delimiter to b and zero-pads it to a multiple of 32 bytes.
func Pack(b []byte) Packed {
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

// Unpack returns the bytes that were packed into p.
//
// Returns an error if it finds an incorrect encoding.
func (p Packed) Unpack() ([]byte, error) {
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

// FromAccessList returns the storage keys of each entry in list at addr, in
// order. Each entry is one packed message, including entries without storage
// keys.
func FromAccessList(list types.AccessList, addr common.Address) []Packed {
	var packed []Packed
	for _, tuple := range list {
		if tuple.Address == addr {
			packed = append(packed, tuple.StorageKeys)
		}
	}
	return packed
}
