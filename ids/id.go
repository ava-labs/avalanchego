// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/cb58"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	IDLen   = 32
	nullStr = "null"
)

var (
	// Empty is a useful all zero value
	Empty = ID{}

	errMissingQuotes = errors.New("first and last characters should be quotes")

	_ utils.Sortable[ID] = ID{}
)

// ID wraps a 32 byte hash used as an identifier
type ID [IDLen]byte

// ToID attempt to convert a byte slice into an id
func ToID(bytes []byte) (ID, error) {
	return hashing.ToHash256(bytes)
}

// FromString is the inverse of ID.String()
func FromString(idStr string) (ID, error) {
	bytes, err := cb58.Decode(idStr)
	if err != nil {
		return ID{}, err
	}
	return ToID(bytes)
}

// FromStringOrPanic is the same as FromString, but will panic on error
func FromStringOrPanic(idStr string) ID {
	id, err := FromString(idStr)
	if err != nil {
		panic(err)
	}
	return id
}

func (id ID) MarshalJSON() ([]byte, error) {
	str, err := cb58.Encode(id[:])
	if err != nil {
		return nil, err
	}
	return []byte(`"` + str + `"`), nil
}

func (id *ID) UnmarshalJSON(b []byte) error {
	str := string(b)
	if str == nullStr { // If "null", do nothing
		return nil
	} else if len(str) < 2 {
		return errMissingQuotes
	}

	lastIndex := len(str) - 1
	if str[0] != '"' || str[lastIndex] != '"' {
		return errMissingQuotes
	}

	// Parse CB58 formatted string to bytes
	bytes, err := cb58.Decode(str[1:lastIndex])
	if err != nil {
		return fmt.Errorf("couldn't decode ID to bytes: %w", err)
	}
	*id, err = ToID(bytes)
	return err
}

func (id *ID) UnmarshalText(text []byte) error {
	return id.UnmarshalJSON(text)
}

// Prefix this id to create a more selective id. This can be used to store
// multiple values under the same key. For example:
// prefix1(id) -> confidence
// prefix2(id) -> vertex
// This will return a new id and not modify the original id.
func (id ID) Prefix(prefixes ...uint64) ID {
	packer := wrappers.Packer{
		Bytes: make([]byte, len(prefixes)*wrappers.LongLen+IDLen),
	}

	for _, prefix := range prefixes {
		packer.PackLong(prefix)
	}
	packer.PackFixedBytes(id[:])

	return hashing.ComputeHash256Array(packer.Bytes)
}

// XOR this id and the provided id and return the resulting id.
//
// Note: this id is not modified.
func (id ID) XOR(other ID) ID {
	for i, b := range other {
		id[i] ^= b
	}
	return id
}

// Bit returns the bit value at the ith index of the byte array. Returns 0 or 1
func (id ID) Bit(i uint) int {
	byteIndex := i / BitsPerByte
	bitIndex := i % BitsPerByte

	b := id[byteIndex]

	// b = [7, 6, 5, 4, 3, 2, 1, 0]

	b >>= bitIndex

	// b = [0, ..., bitIndex + 1, bitIndex]
	// 1 = [0, 0, 0, 0, 0, 0, 0, 1]

	b &= 1

	// b = [0, 0, 0, 0, 0, 0, 0, bitIndex]

	return int(b)
}

// Hex returns a hex encoded string of this id.
func (id ID) Hex() string {
	return hex.EncodeToString(id[:])
}

func (id ID) String() string {
	// We assume that the maximum size of a byte slice that
	// can be stringified is at least the length of an ID
	s, _ := cb58.Encode(id[:])
	return s
}

func (id ID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

func (id ID) Compare(other ID) int {
	return bytes.Compare(id[:], other[:])
}
