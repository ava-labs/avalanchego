// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"reflect"
	"strings"
	"unsafe"
)

const (
	Bit    int = 1
	Crumb  int = 2
	Nibble int = 4
	Byte   int = 8
)

type BranchFactor int

const (
	BranchFactor2   BranchFactor = 2
	BranchFactor4   BranchFactor = 4
	BranchFactor16  BranchFactor = 16
	BranchFactor256 BranchFactor = 256
)

var (
	EmptyPath = func(bf BranchFactor) Path {
		return Path{tokenBitSize: branchFactorToTokenSize[bf]}
	}

	branchFactorToTokenSize = map[BranchFactor]int{
		BranchFactor2:   Bit,
		BranchFactor4:   Crumb,
		BranchFactor16:  Nibble,
		BranchFactor256: Byte,
	}

	tokenSizeToBranchFactor = map[int]BranchFactor{
		Bit:    BranchFactor2,
		Crumb:  BranchFactor4,
		Nibble: BranchFactor16,
		Byte:   BranchFactor256,
	}
)

type Path struct {
	value        string
	tokenBitSize int
}

func NewPath(p []byte, branchFactor BranchFactor) Path {
	result := EmptyPath(branchFactor)

	// create new buffer with a multiple of the input length since each byte gets split into multiple tokens
	tokensPerByte := result.TokensPerByte()
	buffer := make([]byte, len(p)*tokensPerByte)

	// Each token gets bitwise anded with a bit mask to isolate the bits of current token
	// and then shifted right to make it a value between [0, BranchFactor)
	bufferIndex := 0
	for _, currentByte := range p {
		for currentOffset := 0; currentOffset < tokensPerByte; currentOffset++ {
			buffer[bufferIndex+currentOffset] = currentByte & result.mask(currentOffset) >> result.shift(currentOffset)
		}
		bufferIndex += tokensPerByte
	}

	// avoid copying during the conversion
	result.value = *(*string)(unsafe.Pointer(&buffer))
	return result
}

func NewPath16(p []byte) Path {
	return NewPath(p, BranchFactor16)
}

func (p Path) Equal(other Path) bool {
	return p.Compare(other) == 0
}

func (p Path) Compare(other Path) int {
	return strings.Compare(p.value, other.value)
}

func (p Path) Less(other Path) bool {
	return p.Compare(other) == -1
}

func (p Path) Token(index int) byte {
	return p.value[index]
}

func (p Path) Length() int {
	return len(p.value)
}

func (p Path) HasPrefix(prefix Path) bool {
	return strings.HasPrefix(p.value, prefix.value)
}

func (p Path) Append(token byte) Path {
	return Path{
		value:        p.value + string([]byte{token}),
		tokenBitSize: p.tokenBitSize,
	}
}

func (p Path) Extend(path Path) Path {
	return Path{
		value:        p.value + path.value,
		tokenBitSize: p.tokenBitSize,
	}
}

func (p Path) Slice(start, end int) Path {
	return Path{
		value:        p.value[start:end],
		tokenBitSize: p.tokenBitSize,
	}
}

func (p Path) Skip(tokensToSkip int) Path {
	return Path{
		value:        p.value[tokensToSkip:],
		tokenBitSize: p.tokenBitSize,
	}
}

func (p Path) Take(tokensToTake int) Path {
	return Path{
		value:        p.value[:tokensToTake],
		tokenBitSize: p.tokenBitSize,
	}
}

func (p Path) BranchFactor() int {
	return int(tokenSizeToBranchFactor[p.tokenBitSize])
}

func (p Path) TokenBitSize() int {
	return p.tokenBitSize
}

func (p Path) TokensPerByte() int {
	return Byte / p.tokenBitSize
}

// gets the amount of shift(>> or <<) that a token needs to be shifted based on the offset into the current byte
func (p Path) shift(offset int) int {
	return Byte - (p.tokenBitSize * (offset + 1))
}

func (p Path) mask(offset int) byte {
	return byte(tokenSizeToBranchFactor[p.tokenBitSize]-1) << p.shift(offset)
}

// Invariant: The returned value must not be modified.
func (p Path) Bytes() []byte {
	// avoid copying during the conversion
	// "safe" because we never edit the value, only used as DB key
	buf := *(*[]byte)(unsafe.Pointer(&p.value))
	(*reflect.SliceHeader)(unsafe.Pointer(&buf)).Cap = len(p.value)
	return buf
}

// Returns the serialized representation of [p].
func (p Path) Serialize() SerializedPath {
	if len(p.value) == 0 {
		return SerializedPath{
			NibbleLength: 0,
			Value:        []byte{},
		}
	}

	tokensPerByte := p.TokensPerByte()
	byteLength := len(p.value) / tokensPerByte
	remainder := len(p.value) % tokensPerByte

	// add one so there is a byte for the remainder tokens if any exist
	if remainder > 0 {
		byteLength++
	}

	result := SerializedPath{
		NibbleLength: len(p.value),
		Value:        make([]byte, byteLength),
	}

	for pathIndex := 0; pathIndex < len(p.value); pathIndex++ {
		keyIndex := pathIndex / tokensPerByte
		offset := pathIndex % tokensPerByte
		result.Value[keyIndex] += p.value[pathIndex] << p.shift(offset)
	}

	return result
}

// SerializedPath contains a path from the trie.
// The trie branch factor is 16, so the path may contain an odd number of nibbles.
// If it did contain an odd number of nibbles, the last 4 bits of the last byte should be discarded.
type SerializedPath struct {
	NibbleLength int
	Value        []byte
}

func (s SerializedPath) Equal(other SerializedPath) bool {
	return s.NibbleLength == other.NibbleLength && bytes.Equal(s.Value, other.Value)
}

func (s SerializedPath) Deserialize(factor BranchFactor) Path {
	result := NewPath(s.Value, factor)
	return result.Take(s.NibbleLength)
}

// HasPrefix returns true iff [prefix] is a prefix of [s] or equal to it.
func (s SerializedPath) HasPrefix(prefix SerializedPath) bool {
	prefixValue := prefix.Value
	prefixLength := len(prefix.Value)
	if s.NibbleLength < prefix.NibbleLength || len(s.Value) < prefixLength {
		return false
	}
	if prefix.NibbleLength%2 == 0 {
		return bytes.HasPrefix(s.Value, prefixValue)
	}
	reducedSize := prefixLength - 1

	// the input was invalid so just return false
	if reducedSize < 0 {
		return false
	}

	// grab the last nibble in the prefix and serialized path
	prefixRemainder := prefixValue[reducedSize] >> 4
	valueRemainder := s.Value[reducedSize] >> 4
	// s has prefix if the last nibbles are equal and s has every byte but the last of prefix as a prefix
	return valueRemainder == prefixRemainder && bytes.HasPrefix(s.Value, prefixValue[:reducedSize])
}

// Returns true iff [prefix] is a prefix of [s] but not equal to it.
func (s SerializedPath) HasStrictPrefix(prefix SerializedPath) bool {
	return s.HasPrefix(prefix) && !s.Equal(prefix)
}

func (s SerializedPath) NibbleVal(nibbleIndex int) byte {
	value := s.Value[nibbleIndex>>1]
	isOdd := byte(nibbleIndex & 1)
	isEven := 1 - isOdd

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
