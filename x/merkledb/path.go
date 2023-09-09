// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"reflect"
	"strings"
	"unsafe"

	"golang.org/x/exp/slices"
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

	var deserializeFunc func(val byte, pathIndex int, buffer []byte)
	switch branchFactor {
	case BranchFactor2:
		deserializeFunc = deserializeByte2
	case BranchFactor4:
		deserializeFunc = deserializeByte4
	case BranchFactor16:
		deserializeFunc = deserializeByte16
	case BranchFactor256:
		return Path{
			value:        string(p),
			tokenBitSize: Byte,
		}
	}

	pathIndex := 0
	for _, currentByte := range p {
		deserializeFunc(currentByte, pathIndex, buffer)
		pathIndex += tokensPerByte
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
	var serializeFunc func(s *SerializedPath, byteIndex int, pathIndex int, p Path)
	switch p.TokenBitSize() {
	case Bit:
		serializeFunc = serializeByte2
	case Crumb:
		serializeFunc = serializeByte4
	case Nibble:
		serializeFunc = serializeByte16
	case Byte:
		return SerializedPath{
			NibbleLength: len(p.value),
			Value:        slices.Clone(p.Bytes()),
		}
	}

	tokensPerByte := p.TokensPerByte()
	byteLength := len(p.value) / tokensPerByte
	remainder := len(p.value) % tokensPerByte
	remainderAddition := 0
	// add one so there is a byte for the remainder tokens if any exist
	if remainder > 0 {
		remainderAddition = 1
	}

	result := SerializedPath{
		NibbleLength: len(p.value),
		Value:        make([]byte, byteLength+remainderAddition),
	}

	// handle full bytes
	pathIndex := 0
	for byteIndex := 0; byteIndex < byteLength; byteIndex++ {
		serializeFunc(&result, byteIndex, pathIndex, p)
		pathIndex += tokensPerByte
	}

	// deal with any partial byte due to remainder
	for offset := 0; offset < remainder; offset++ {
		result.Value[byteLength] += p.value[pathIndex+offset] << p.shift(offset)
	}

	return result
}

func deserializeByte2(val byte, pathIndex int, buffer []byte) {
	buffer[pathIndex] = val >> 7
	buffer[pathIndex+1] = (val & 0b0100_0000) >> 6
	buffer[pathIndex+2] = (val & 0b0010_0000) >> 5
	buffer[pathIndex+3] = (val & 0b0001_0000) >> 4
	buffer[pathIndex+4] = (val & 0b0000_1000) >> 3
	buffer[pathIndex+5] = (val & 0b0000_0100) >> 2
	buffer[pathIndex+6] = (val & 0b0000_0010) >> 1
	buffer[pathIndex+7] = val & 0b0000_0001
}

func deserializeByte4(val byte, pathIndex int, buffer []byte) {
	buffer[pathIndex] = val >> 6
	buffer[pathIndex+1] = (val & 0b0011_0000) >> 4
	buffer[pathIndex+2] = (val & 0b0000_1100) >> 2
	buffer[pathIndex+3] = val & 0b0000_0011
}

func deserializeByte16(val byte, pathIndex int, buffer []byte) {
	buffer[pathIndex] = val >> 4
	buffer[pathIndex+1] = val & 0b0000_1111
}

func serializeByte2(s *SerializedPath, byteIndex int, pathIndex int, p Path) {
	s.Value[byteIndex] += p.value[pathIndex]<<7 +
		p.value[pathIndex+1]<<6 +
		p.value[pathIndex+2]<<5 +
		p.value[pathIndex+3]<<4 +
		p.value[pathIndex+4]<<3 +
		p.value[pathIndex+5]<<2 +
		p.value[pathIndex+6]<<1 +
		p.value[pathIndex+7]
}

func serializeByte4(s *SerializedPath, byteIndex int, pathIndex int, p Path) {
	s.Value[byteIndex] += p.value[pathIndex]<<6 +
		p.value[pathIndex+1]<<4 +
		p.value[pathIndex+2]<<2 +
		p.value[pathIndex+3]<<0
}

func serializeByte16(s *SerializedPath, byteIndex int, pathIndex int, p Path) {
	s.Value[byteIndex] += p.value[pathIndex]<<4 +
		p.value[pathIndex+1]
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
