// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"reflect"
	"strings"
	"unsafe"
)

const EmptyPath path = ""

// SerializedPath contains a path from the trie.
// The trie branch factor is 16, so the path may contain an odd number of nibbles.
// If it did contain an odd number of nibbles, the last 4 bits of the last byte should be discarded.
type SerializedPath struct {
	NibbleLength int
	Value        []byte
}

func (s SerializedPath) hasOddLength() bool {
	return s.NibbleLength&1 == 1
}

func (s SerializedPath) Equal(other SerializedPath) bool {
	return s.NibbleLength == other.NibbleLength && bytes.Equal(s.Value, other.Value)
}

func (s SerializedPath) deserialize() path {
	result := newPath(s.Value)
	// trim the last nibble if the path has an odd length
	return result[:len(result)-s.NibbleLength&1]
}

// Returns true iff [prefix] is a prefix of [s] or equal to it.
func (s SerializedPath) HasPrefix(prefix SerializedPath) bool {
	if len(s.Value) < len(prefix.Value) {
		return false
	}
	prefixValue := prefix.Value
	if !prefix.hasOddLength() {
		return bytes.HasPrefix(s.Value, prefixValue)
	}
	reducedSize := len(prefixValue) - 1

	// grab the last nibble in the prefix and serialized path
	prefixRemainder := prefixValue[reducedSize] >> 4
	valueRemainder := s.Value[reducedSize] >> 4
	prefixValue = prefixValue[:reducedSize]
	return bytes.HasPrefix(s.Value, prefixValue) && valueRemainder == prefixRemainder
}

// Returns true iff [prefix] is a prefix of [s] but not equal to it.
func (s SerializedPath) HasStrictPrefix(prefix SerializedPath) bool {
	return s.HasPrefix(prefix) && !s.Equal(prefix)
}

func (s SerializedPath) NibbleVal(nibbleIndex int) byte {
	value := s.Value[nibbleIndex>>1]
	isOdd := byte(nibbleIndex & 1)
	isEven := (1 - isOdd)

	// return value first(even index) or last 4(odd index) bits of the corresponding byte
	return isEven*value>>4 + isOdd*(value&0x0F)
}

func (s SerializedPath) AppendNibble(nibble byte) SerializedPath {
	// even is 1 if even, 0 if odd
	even := 1 - s.NibbleLength&1
	value := make([]byte, len(s.Value)+even)
	copy(value, s.Value)

	// shift the nibble 4 left if even, do nothing if odd
	value[len(value)-1] += nibble << (4 * even)
	return SerializedPath{Value: value, NibbleLength: s.NibbleLength + 1}
}

type path string

// Returns:
// * 0 if [p] == [other].
// * -1 if [p] < [other].
// * 1 if [p] > [other].
func (p path) Compare(other path) int {
	return strings.Compare(string(p), string(other))
}

// Invariant: The returned value must not be modified.
func (p path) Bytes() []byte {
	// avoid copying during the conversion
	// "safe" because we never edit the value, only used as DB key
	buf := *(*[]byte)(unsafe.Pointer(&p))
	(*reflect.SliceHeader)(unsafe.Pointer(&buf)).Cap = len(p)
	return buf
}

// Returns true iff [p] begins with [prefix].
func (p path) HasPrefix(prefix path) bool {
	return strings.HasPrefix(string(p), string(prefix))
}

// Append [val] to [p].
func (p path) Append(val byte) path {
	return p + path(val)
}

// Returns the serialized representation of [p].
func (p path) Serialize() SerializedPath {
	// need half the number of bytes as nibbles
	// add one so there is a byte for the odd nibble if it exists
	// the extra nibble gets rounded down if even length
	byteLength := (len(p) + 1) / 2

	result := SerializedPath{
		NibbleLength: len(p),
		Value:        make([]byte, byteLength),
	}

	// loop over the path's bytes
	// if the length is odd, subtract 1 so we don't overflow on the p[pathIndex+1]
	keyIndex := 0
	lastIndex := len(p) - len(p)&1
	for pathIndex := 0; pathIndex < lastIndex; pathIndex += 2 {
		result.Value[keyIndex] = p[pathIndex]<<4 + p[pathIndex+1]
		keyIndex++
	}

	// if there is was a odd number of nibbles, grab the last one
	if result.hasOddLength() {
		result.Value[keyIndex] = p[keyIndex<<1] << 4
	}

	return result
}

func newPath(p []byte) path {
	// create new buffer with double the length of the input since each byte gets split into two nibbles
	buffer := make([]byte, 2*len(p))

	// first nibble gets shifted right 4 (divided by 16) to isolate the first nibble
	// second nibble gets bitwise anded with 0x0F (1111) to isolate the second nibble
	bufferIndex := 0
	for _, currentByte := range p {
		buffer[bufferIndex] = currentByte >> 4
		buffer[bufferIndex+1] = currentByte & 0x0F
		bufferIndex += 2
	}

	// avoid copying during the conversion
	return *(*path)(unsafe.Pointer(&buffer))
}
