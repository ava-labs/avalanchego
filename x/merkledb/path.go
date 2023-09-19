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
	BranchFactorUnspecified BranchFactor = 0
	BranchFactor2           BranchFactor = 2
	BranchFactor4           BranchFactor = 4
	BranchFactor16          BranchFactor = 16
	BranchFactor256         BranchFactor = 256
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
		BranchFactorUnspecified: {},
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

// Length returns the number of tokens in the current Path
func (cp Path) Length() int {
	return cp.length
}

// hasPartialByteLength returns true iff the path fits into a non whole number of bytes
func (cp Path) hasPartialByteLength() bool {
	return cp.length%cp.tokensPerByte > 0
}

// HasPrefix returns true iff [prefix] is a prefix of [s] or equal to it.
func (cp Path) HasPrefix(prefix Path) bool {
	prefixValue := prefix.value
	prefixLength := len(prefix.value)
	if cp.length < prefix.length || len(cp.value) < prefixLength {
		return false
	}
	remainderTokens := prefix.length % cp.tokensPerByte
	if remainderTokens == 0 {
		return strings.HasPrefix(cp.value, prefixValue)
	}
	reducedSize := prefixLength - 1

	// the input was invalid so just return false
	if reducedSize < 0 {
		return false
	}
	for i := prefix.length - remainderTokens; i < prefix.length; i++ {
		if cp.Token(i) != prefix.Token(i) {
			return false
		}
	}
	// s has prefix if the partial byte tokens are equal and s has every byte but the remainder tokens of prefix as a prefix
	return strings.HasPrefix(cp.value, prefixValue[:reducedSize])
}

// HasStrictPrefix Returns true iff [prefix] is a prefix of [s] but not equal to it.
func (cp Path) HasStrictPrefix(prefix Path) bool {
	return cp.HasPrefix(prefix) && !cp.Equals(prefix)
}

// Token returns the token at the specified index
// grabs the token's storage byte, shifts it, then masks out any bits from other tokens stored in the same byte
func (cp Path) Token(index int) byte {
	return (cp.value[index/cp.tokensPerByte] >> cp.shift(index)) & cp.mask
}

// Append returns a new Path that equals the current Path with the passed token appended to the end
func (cp Path) Append(token byte) Path {
	buffer := make([]byte, cp.bytesNeeded(cp.length+1))
	copy(buffer, cp.value)
	buffer[len(buffer)-1] += token << cp.shift(cp.length)
	return Path{
		value:      *(*string)(unsafe.Pointer(&buffer)),
		length:     cp.length + 1,
		pathConfig: cp.pathConfig,
	}
}

// Greater returns true if current Path is greater than other Path
func (cp Path) Greater(other Path) bool {
	return cp.value > other.value || (cp.value == other.value && cp.length > other.length)
}

// Equals returns true if current Path is equal to other Path
func (cp Path) Equals(other Path) bool {
	return cp.value == other.value && cp.length == other.length
}

// Less returns true if current Path is less than other Path
func (cp Path) Less(other Path) bool {
	return cp.value < other.value || (cp.value == other.value && cp.length < other.length)
}

// shift indicates how many bits to shift the token at the index to get the raw token value
// varies from (8-[tokenBitSize]) -> (0)
// when remainder of index varies from (0) -> (cp.tokensPerByte-1)
func (cp Path) shift(index int) byte {
	return Byte - (cp.tokenBitSize * byte(index%cp.tokensPerByte+1))
}

// bytesNeeded returns the number of bytes needed to store the passed number of tokens
func (cp Path) bytesNeeded(tokens int) int {
	// (cp.tokensPerByte - 1) ensures that it rounds up
	// acts a Ceiling
	return (tokens + cp.tokensPerByte - 1) / cp.tokensPerByte
}

// Extend returns a new Path that equals the passed Path appended to the current Path
func (cp Path) Extend(path Path) Path {
	if cp.length == 0 {
		return path
	}
	if path.length == 0 {
		return cp
	}

	totalLength := cp.length + path.length
	buffer := make([]byte, cp.bytesNeeded(totalLength))
	copy(buffer, cp.value)
	if !cp.hasPartialByteLength() {
		copy(buffer[len(cp.value):], path.value)
		return Path{
			value:      *(*string)(unsafe.Pointer(&buffer)),
			length:     totalLength,
			pathConfig: cp.pathConfig,
		}
	}
	shiftLeft := cp.shift(cp.length - 1)
	shiftRight := Byte - shiftLeft
	buffer[len(cp.value)-1] += path.value[0] >> shiftRight

	for i := 0; i < len(path.value)-1; i++ {
		buffer[len(cp.value)+i] += path.value[i]<<shiftLeft + path.value[i+1]>>shiftRight
	}
	buffer[len(buffer)-1] += path.value[len(path.value)-1] << shiftLeft

	return Path{
		value:      *(*string)(unsafe.Pointer(&buffer)),
		length:     cp.length + path.length,
		pathConfig: cp.pathConfig,
	}
}

// Skip returns a new Path that contains the last cp.length-tokensToSkip tokens of the current Path
func (cp Path) Skip(tokensToSkip int) Path {
	newLength := cp.length - tokensToSkip
	result := Path{
		length:     newLength,
		pathConfig: cp.pathConfig,
	}
	if newLength == 0 {
		return result
	}

	remainingBytes := cp.value[tokensToSkip/cp.tokensPerByte:]

	if tokensToSkip%cp.tokensPerByte == 0 {
		result.value = remainingBytes
		return result
	}

	buffer := make([]byte, cp.bytesNeeded(newLength))
	shiftRight := cp.shift(tokensToSkip - 1)
	shiftLeft := Byte - shiftRight

	for i := 0; i < len(remainingBytes)-1; i++ {
		buffer[i] += remainingBytes[i]<<shiftLeft + remainingBytes[i+1]>>shiftRight
	}
	buffer[len(buffer)-1] += remainingBytes[len(remainingBytes)-1] << shiftLeft

	return Path{
		value:      *(*string)(unsafe.Pointer(&buffer)),
		length:     newLength,
		pathConfig: cp.pathConfig,
	}
}

// Take returns a new Path that contains the first tokensToTake tokens of the current Path
func (cp Path) Take(tokensToTake int) Path {
	if tokensToTake%cp.tokensPerByte == 0 {
		return Path{
			value:      cp.value[:tokensToTake/cp.tokensPerByte],
			length:     tokensToTake,
			pathConfig: cp.pathConfig,
		}
	}

	// ensure length rounds up
	buffer := make([]byte, cp.bytesNeeded(tokensToTake))
	copy(buffer, cp.value)
	shift := cp.shift(tokensToTake - 1)

	// zero out remainder bits
	buffer[len(buffer)-1] = (buffer[len(buffer)-1] >> shift) << shift

	return Path{
		value:      *(*string)(unsafe.Pointer(&buffer)),
		length:     tokensToTake,
		pathConfig: cp.pathConfig,
	}
}

func (cp Path) BranchFactor() BranchFactor {
	if cp.pathConfig != nil {
		return cp.branchFactor
	}
	return BranchFactorUnspecified
}

// Bytes returns the raw bytes of the Path
// Invariant: The returned value must not be modified.
func (cp Path) Bytes() []byte {
	// avoid copying during the conversion
	// "safe" because we never edit the value, only used as DB key
	buf := *(*[]byte)(unsafe.Pointer(&cp.value))
	(*reflect.SliceHeader)(unsafe.Pointer(&buf)).Cap = len(cp.value)
	return buf
}
