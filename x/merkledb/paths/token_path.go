// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package paths

import (
	"reflect"
	"strings"
	"unsafe"
)

var (
	EmptyPath = map[int]TokenPath{
		2:   {branchFactor: 2},
		4:   {branchFactor: 4},
		16:  {branchFactor: 16},
		256: {branchFactor: 256},
	}
	branchFactorToTokenCount = map[int]int{2: 8, 4: 4, 16: 2, 256: 1}
	branchToOffsetMasks      = map[int]map[int]byte{
		2:   {0: 0b1000_0000, 1: 0b0100_0000, 2: 0b0010_0000, 3: 0b0001_0000, 4: 0b0000_1000, 5: 0b0000_0100, 6: 0b0000_0010, 7: 0b0000_0001},
		4:   {0: 0b1100_0000, 1: 0b0011_0000, 2: 0b0000_1100, 3: 0b0000_0011},
		16:  {1: 0x0F, 0: 0xF0},
		256: {0: 0xFF}}
	branchToOffsetShifts = map[int]map[int]byte{
		2:   {0: 7, 1: 6, 2: 5, 3: 4, 4: 3, 5: 2, 6: 1, 7: 0},
		4:   {0: 6, 1: 4, 2: 2, 3: 0},
		16:  {0: 4, 1: 0},
		256: {0: 0}}
)

type TokenPath struct {
	value        string
	branchFactor int
}

func NewTokenPath(p []byte, branchFactor int) TokenPath {
	// create new buffer with double the length of the input since each byte gets split into two nibbles
	tokensPerByte := branchFactorToTokenCount[branchFactor]
	buffer := make([]byte, len(p)*tokensPerByte)

	// first nibble gets shifted right 4 (divided by 16) to isolate the first nibble
	// second nibble gets bitwise anded with 0x0F (1111) to isolate the second nibble
	bufferIndex := 0
	for _, currentByte := range p {
		for currentOffset := 0; currentOffset < tokensPerByte; currentOffset++ {
			mask := branchToOffsetMasks[branchFactor][currentOffset]
			shift := branchToOffsetShifts[branchFactor][currentOffset]
			buffer[bufferIndex+currentOffset] = currentByte & mask >> shift
		}
		bufferIndex += tokensPerByte
	}

	// avoid copying during the conversion
	return TokenPath{
		value:        *(*string)(unsafe.Pointer(&buffer)),
		branchFactor: branchFactor,
	}
}

func (p TokenPath) Equal(other TokenPath) bool {
	return p.Compare(other) == 0
}

func (p TokenPath) Compare(other TokenPath) int {
	return strings.Compare(p.value, other.value)
}

func (p TokenPath) Less(other TokenPath) bool {
	return p.Compare(other) == -1
}

func (p TokenPath) Token(index int) byte {
	return p.value[index]
}

func (p TokenPath) Length() int {
	return len(p.value)
}

func (p TokenPath) HasPrefix(prefix TokenPath) bool {
	return strings.HasPrefix(p.value, prefix.value)
}

func (p TokenPath) Append(token byte) TokenPath {
	return TokenPath{
		value:        p.value + string(token),
		branchFactor: p.branchFactor,
	}
}

func (p TokenPath) Extend(path TokenPath) TokenPath {
	return TokenPath{
		value:        p.value + path.value,
		branchFactor: p.branchFactor,
	}
}

func (p TokenPath) Slice(start, end int) TokenPath {
	return TokenPath{
		value:        p.value[start:end],
		branchFactor: p.branchFactor,
	}
}

func (p TokenPath) Skip(tokensToSkip int) TokenPath {
	return TokenPath{
		value:        p.value[tokensToSkip:],
		branchFactor: p.branchFactor,
	}
}

func (p TokenPath) Take(tokensToTake int) TokenPath {
	return TokenPath{
		value:        p.value[:tokensToTake],
		branchFactor: p.branchFactor,
	}
}

func (p TokenPath) TokensPerByte() int {
	return branchFactorToTokenCount[p.branchFactor]
}

// Invariant: The returned value must not be modified.
func (p TokenPath) Bytes() []byte {
	// avoid copying during the conversion
	// "safe" because we never edit the value, only used as DB key
	buf := *(*[]byte)(unsafe.Pointer(&p.value))
	(*reflect.SliceHeader)(unsafe.Pointer(&buf)).Cap = len(p.value)
	return buf
}

// Returns the serialized representation of [p].
func (p TokenPath) Serialize() SerializedPath {
	if len(p.value) == 0 {
		return SerializedPath{
			NibbleLength: 0,
			Value:        []byte{},
		}
	}
	// add one so there is a byte for the odd nibble if it exists
	tokensPerByte := branchFactorToTokenCount[p.branchFactor]
	byteLength := len(p.value) / tokensPerByte
	remainder := len(p.value) % tokensPerByte
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
		result.Value[keyIndex] += p.value[pathIndex] << branchToOffsetShifts[p.branchFactor][offset]
		keyIndex++
	}

	return result
}

func NewNibblePath(p []byte) TokenPath {
	return NewTokenPath(p, 16)
}
