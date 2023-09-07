// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package path

import (
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
	EmptyPath = func(bf BranchFactor) TokenPath {
		return TokenPath{tokenBitSize: branchFactorToTokenSize[bf]}
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

type TokenPath struct {
	value        string
	tokenBitSize int
}

func NewTokenPath(p []byte, branchFactor BranchFactor) TokenPath {
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

func NewTokenPath16(p []byte) TokenPath {
	return NewTokenPath(p, BranchFactor16)
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
		value:        p.value + string([]byte{token}),
		tokenBitSize: p.tokenBitSize,
	}
}

func (p TokenPath) Extend(path TokenPath) TokenPath {
	return TokenPath{
		value:        p.value + path.value,
		tokenBitSize: p.tokenBitSize,
	}
}

func (p TokenPath) Slice(start, end int) TokenPath {
	return TokenPath{
		value:        p.value[start:end],
		tokenBitSize: p.tokenBitSize,
	}
}

func (p TokenPath) Skip(tokensToSkip int) TokenPath {
	return TokenPath{
		value:        p.value[tokensToSkip:],
		tokenBitSize: p.tokenBitSize,
	}
}

func (p TokenPath) Take(tokensToTake int) TokenPath {
	return TokenPath{
		value:        p.value[:tokensToTake],
		tokenBitSize: p.tokenBitSize,
	}
}

func (p TokenPath) BranchFactor() int {
	return int(tokenSizeToBranchFactor[p.tokenBitSize])
}

func (p TokenPath) TokenBitSize() int {
	return p.tokenBitSize
}

func (p TokenPath) TokensPerByte() int {
	return Byte / p.tokenBitSize
}

// gets the amount of shift(>> or <<) that a token needs to be shifted based on the offset into the current byte
func (p TokenPath) shift(offset int) int {
	return Byte - (p.tokenBitSize * (offset + 1))
}

func (p TokenPath) mask(offset int) byte {
	return byte(tokenSizeToBranchFactor[p.tokenBitSize]-1) << p.shift(offset)
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
