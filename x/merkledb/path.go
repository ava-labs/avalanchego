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
	switch branchFactor {
	case BranchFactor2:
		return NewPath2(p)
	case BranchFactor4:
		return NewPath4(p)
	case BranchFactor16:
		return NewPath16(p)
	case BranchFactor256:
		return NewPath256(p)
	}
	return EmptyPath(branchFactor)
}

func NewPath2(p []byte) Path {
	result := EmptyPath(BranchFactor2)
	buffer := make([]byte, len(p)*8)

	pathIndex := 0
	for _, currentByte := range p {
		buffer[pathIndex] = currentByte >> 7
		buffer[pathIndex+1] = (currentByte & 0b0100_0000) >> 6
		buffer[pathIndex+2] = (currentByte & 0b0010_0000) >> 5
		buffer[pathIndex+3] = (currentByte & 0b0001_0000) >> 4
		buffer[pathIndex+4] = (currentByte & 0b0000_1000) >> 3
		buffer[pathIndex+5] = (currentByte & 0b0000_0100) >> 2
		buffer[pathIndex+6] = (currentByte & 0b0000_0010) >> 1
		buffer[pathIndex+7] = currentByte & 0b0000_0001
		pathIndex += 8
	}

	// avoid copying during the conversion
	result.value = *(*string)(unsafe.Pointer(&buffer))
	return result
}

func NewPath4(p []byte) Path {
	result := EmptyPath(BranchFactor4)
	buffer := make([]byte, len(p)*4)

	pathIndex := 0
	for _, currentByte := range p {
		buffer[pathIndex] = currentByte >> 6
		buffer[pathIndex+1] = (currentByte & 0b0011_0000) >> 4
		buffer[pathIndex+2] = (currentByte & 0b0000_1100) >> 2
		buffer[pathIndex+3] = currentByte & 0b0000_0011
		pathIndex += 4
	}

	// avoid copying during the conversion
	result.value = *(*string)(unsafe.Pointer(&buffer))
	return result
}

func NewPath16(p []byte) Path {
	result := EmptyPath(BranchFactor16)
	buffer := make([]byte, len(p)*2)

	pathIndex := 0
	for _, currentByte := range p {
		buffer[pathIndex] = currentByte >> 4
		buffer[pathIndex+1] = currentByte & 0b0000_1111
		pathIndex += 2
	}

	// avoid copying during the conversion
	result.value = *(*string)(unsafe.Pointer(&buffer))
	return result
}

func NewPath256(p []byte) Path {
	return Path{
		value:        string(p),
		tokenBitSize: Byte,
	}
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

func (p Path) TokensPerByte() int {
	return Byte / p.tokenBitSize
}

// Invariant: The returned value must not be modified.
func (p Path) Bytes() []byte {
	// avoid copying during the conversion
	// "safe" because we never edit the value, only used as DB key
	buf := *(*[]byte)(unsafe.Pointer(&p.value))
	(*reflect.SliceHeader)(unsafe.Pointer(&buf)).Cap = len(p.value)
	return buf
}

func (p Path) Serialize() SerializedPath {
	tokensPerByte := 0
	var setFunc func(pathIndex int) byte
	switch p.BranchFactor() {
	case 2:
		setFunc = p.serialize2
		tokensPerByte = 8
	case 4:
		setFunc = p.serialize4
		tokensPerByte = 4
	case 16:
		setFunc = p.serialize16
		tokensPerByte = 2
	case 256:
		setFunc = p.serialize256
		tokensPerByte = 1
	default:
		return SerializedPath{}
	}
	remainder := len(p.value) % tokensPerByte

	result := SerializedPath{
		NibbleLength: len(p.value),
		// Ensure that this rounds up when len(p.value) is not evenly divisible by tokensPerByte
		// so that the extra byte stores any remainder tokens
		Value: make([]byte, (len(p.value)+tokensPerByte-1)/tokensPerByte),
	}

	keyIndex := 0
	pathIndex := 0
	// loop over the path's bytes
	// if the length has a remainder, subtract it so we don't overflow on the p[pathIndex+1]
	lastIndex := len(p.value) - remainder
	for ; pathIndex < lastIndex; pathIndex += tokensPerByte {
		result.Value[keyIndex] = setFunc(pathIndex)
		keyIndex++
	}

	shift := Byte - p.tokenBitSize
	for offset := 0; offset < remainder; offset++ {
		result.Value[keyIndex] += p.value[pathIndex+offset] << shift
		shift -= p.tokenBitSize
	}

	return result
}

func (p Path) serialize2(pathIndex int) byte {
	return p.value[pathIndex]<<7 +
		p.value[pathIndex+1]<<6 +
		p.value[pathIndex+2]<<5 +
		p.value[pathIndex+3]<<4 +
		p.value[pathIndex+4]<<3 +
		p.value[pathIndex+5]<<2 +
		p.value[pathIndex+6]<<1 +
		p.value[pathIndex+7]
}

func (p Path) serialize4(pathIndex int) byte {
	return p.value[pathIndex]<<6 +
		p.value[pathIndex+1]<<4 +
		p.value[pathIndex+2]<<2 +
		p.value[pathIndex+3]<<0
}

func (p Path) serialize16(pathIndex int) byte {
	return p.value[pathIndex]<<4 +
		p.value[pathIndex+1]
}

func (p Path) serialize256(pathIndex int) byte {
	return p.value[pathIndex]
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
