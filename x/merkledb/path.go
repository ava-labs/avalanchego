// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"reflect"
	"strings"
	"unsafe"
)

const (
	Bit    byte = 1
	Crumb  byte = 2
	Nibble byte = 4
	Byte   byte = 8
)

type BranchFactor int

const (
	BranchFactor2   BranchFactor = 2
	BranchFactor4   BranchFactor = 4
	BranchFactor16  BranchFactor = 16
	BranchFactor256 BranchFactor = 256
)

type pathConfig struct {
	branchFactor  BranchFactor
	tokensPerByte int
	tokenBitSize  byte
	mask          byte
}

type Path struct {
	length int
	value  string
	*pathConfig
}

var (
	branchFactorToPathConfig = map[BranchFactor]*pathConfig{
		BranchFactor2: {
			branchFactor:  BranchFactor2,
			tokenBitSize:  Bit,
			tokensPerByte: 8,
			mask:          0b0000_0001,
		},
		BranchFactor4: {
			branchFactor:  BranchFactor4,
			tokenBitSize:  Crumb,
			tokensPerByte: 4,
			mask:          0b0000_0011,
		},
		BranchFactor16: {
			branchFactor:  BranchFactor16,
			tokenBitSize:  Nibble,
			tokensPerByte: 2,
			mask:          0b0000_1111,
		},
		BranchFactor256: {
			branchFactor:  BranchFactor256,
			tokenBitSize:  Byte,
			tokensPerByte: 1,
			mask:          0b1111_1111,
		},
	}

	EmptyPath = func(bf BranchFactor) Path {
		return Path{pathConfig: branchFactorToPathConfig[bf]}
	}
)

func NewPath(p []byte, branchFactor BranchFactor) Path {
	result := EmptyPath(branchFactor)
	result.value = string(p)
	result.length = len(p) * result.tokensPerByte
	return result
}

func NewPath16(p []byte) Path {
	return NewPath(p, BranchFactor16)
}

func (cp Path) Equal(other Path) bool {
	return cp.length == other.length && cp.value == other.value
}

// HasPrefix returns true iff [prefix] is a prefix of [s] or equal to it.
func (cp Path) HasPrefix(prefix Path) bool {
	prefixValue := prefix.value
	prefixLength := len(prefix.value)
	if cp.length < prefix.length || len(cp.value) < prefixLength {
		return false
	}
	remainder := prefix.length % cp.tokensPerByte
	if remainder == 0 {
		return strings.HasPrefix(cp.value, prefixValue)
	}
	reducedSize := prefixLength - 1

	// the input was invalid so just return false
	if reducedSize < 0 {
		return false
	}
	for i := prefix.length - remainder; i < prefix.length; i++ {
		if cp.Token(i) != prefix.Token(i) {
			return false
		}
	}
	// s has prefix if the last nibbles are equal and s has every byte but the last of prefix as a prefix
	return strings.HasPrefix(cp.value, prefixValue[:reducedSize])
}

// Returns true iff [prefix] is a prefix of [s] but not equal to it.
func (cp Path) HasStrictPrefix(prefix Path) bool {
	return cp.HasPrefix(prefix) && !cp.Equal(prefix)
}

func (cp Path) Token(index int) byte {
	offset := index % cp.tokensPerByte
	return (cp.value[index/cp.tokensPerByte] >> cp.shift(offset)) & cp.mask
}

func (cp Path) Append(token byte) Path {
	buffer := make([]byte, (cp.length+1)/cp.tokensPerByte)
	copy(buffer, cp.value)
	buffer[len(buffer)-1] += token << cp.shift((cp.length+1)%cp.tokensPerByte)
	return Path{value: *(*string)(unsafe.Pointer(&buffer)), length: cp.length + 1}
}

func (cp Path) Compare(other Path) int {
	result := strings.Compare(cp.value, other.value)
	if result != 0 {
		return result
	}
	switch {
	case cp.length < other.Length():
		return -1
	case cp.length == other.Length():
		return 0
	}
	return 1
}

func (cp Path) shift(offset int) byte {
	return Byte - (cp.tokenBitSize * byte(offset+1))
}

func (cp Path) Less(other Path) bool {
	return cp.Compare(other) == -1
}

func (cp Path) Length() int {
	return cp.length
}

func (cp Path) Extend(path Path) Path {
	remainder := cp.length % cp.tokensPerByte
	totalLength := cp.length + path.length
	buffer := make([]byte, (totalLength+cp.tokensPerByte-1)/cp.tokensPerByte)
	copy(buffer, cp.value)
	if remainder == 0 {
		copy(buffer[len(cp.value):], path.value)
		return Path{
			value:      *(*string)(unsafe.Pointer(&buffer)),
			length:     totalLength,
			pathConfig: cp.pathConfig,
		}
	}
	odd := len(path.value) & 1
	stopIndex := len(path.value) - odd
	shift := cp.shift(remainder - 1)
	reverseShift := Byte - shift
	buffer[len(cp.value)-1] += (path.value[0] & (cp.mask ^ 0xFF)) >> reverseShift

	for i := 0; i < stopIndex-1; i++ {
		buffer[i+len(cp.value)] += path.value[i]<<shift + (path.value[i+1]&(cp.mask^0xFF))>>reverseShift
	}
	if odd == 1 {
		buffer[stopIndex+len(cp.value)] += path.value[stopIndex] << shift
	}

	return Path{
		value:      *(*string)(unsafe.Pointer(&buffer)),
		length:     cp.length + path.length,
		pathConfig: cp.pathConfig,
	}
}

func (cp Path) Slice(start, end int) Path {
	panic("not implemented")
}

func (cp Path) Skip(tokensToSkip int) Path {
	panic("not implemented")
}

func (cp Path) Take(tokensToTake int) Path {
	return Path{
		// ensure that the index rounds up
		value:      cp.value[:(tokensToTake+cp.tokensPerByte-1)/cp.tokensPerByte],
		length:     tokensToTake,
		pathConfig: cp.pathConfig,
	}
}

// Invariant: The returned value must not be modified.
func (cp Path) Bytes() []byte {
	// avoid copying during the conversion
	// "safe" because we never edit the value, only used as DB key
	buf := *(*[]byte)(unsafe.Pointer(&cp.value))
	(*reflect.SliceHeader)(unsafe.Pointer(&buf)).Cap = len(cp.value)
	return buf
}
