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
	pConfig := branchFactorToPathConfig[branchFactor]
	return Path{
		value:      string(p),
		pathConfig: pConfig,
		length:     len(p) * pConfig.tokensPerByte,
	}
}

// Length returns the number of tokens in the current Path
func (cp Path) Length() int {
	return cp.length
}

// hasPartialByte returns true iff the path fits into a non-whole number of bytes
func (cp Path) hasPartialByte() bool {
	return cp.length%cp.tokensPerByte > 0
}

// HasPrefix returns true iff [prefix] is a prefix of [s] or equal to it.
func (cp Path) HasPrefix(prefix Path) bool {
	// if the current path is shorter than the prefix path, then it cannot be a prefix
	if cp.length < prefix.length || len(cp.value) < len(prefix.value) {
		return false
	}

	remainderTokens := prefix.length % cp.tokensPerByte
	if remainderTokens == 0 {
		return strings.HasPrefix(cp.value, prefix.value)
	}

	// check that the tokens that were in the partially filled prefix byte are equal to the tokens in the current path
	for i := prefix.length - remainderTokens; i < prefix.length; i++ {
		if cp.Token(i) != prefix.Token(i) {
			return false
		}
	}

	// the prefix contains a partially filled byte so grab the length of whole bytes
	wholeBytesLength := len(prefix.value) - 1

	// the input was valid and the whole bytes of the prefix are a prefix of the current path
	return wholeBytesLength >= 0 && strings.HasPrefix(cp.value, prefix.value[:wholeBytesLength])
}

// HasStrictPrefix Returns true iff [prefix] is a prefix of [s] but not equal to it.
func (cp Path) HasStrictPrefix(prefix Path) bool {
	return cp.HasPrefix(prefix) && !cp.Equals(prefix)
}

// Token returns the token at the specified index
// grabs the token's storage byte, shifts it, then masks out any bits from other tokens stored in the same byte
func (cp Path) Token(index int) byte {
	return (cp.value[index/cp.tokensPerByte] >> cp.bitsToShift(index)) & cp.mask
}

// Append returns a new Path that equals the current Path with the passed token appended to the end
func (cp Path) Append(token byte) Path {
	buffer := make([]byte, cp.bytesNeeded(cp.length+1))
	copy(buffer, cp.value)
	buffer[len(buffer)-1] |= token << cp.bitsToShift(cp.length)
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

// bitsToShift indicates how many bits to shift the token at the index to get the raw token value
// varies from (8-[tokenBitSize]) -> (0) when remainder of index varies from (0) -> (cp.tokensPerByte-1)
func (cp Path) bitsToShift(index int) byte {
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

	// copy existing value into  the buffer
	buffer := make([]byte, cp.bytesNeeded(totalLength))
	copy(buffer, cp.value)

	// If the existing value fits into a whole number of bytes,
	// the extension path can be copied directly into the buffer.
	if !cp.hasPartialByte() {
		copy(buffer[len(cp.value):], path.value)
		return Path{
			value:      *(*string)(unsafe.Pointer(&buffer)),
			length:     totalLength,
			pathConfig: cp.pathConfig,
		}
	}

	// The existing path doesn't fit into a whole number of bytes,
	// figure out how much each byte of the extension path needs to be shifted
	shiftLeft := cp.bitsToShift(cp.length - 1)
	shiftRight := Byte - shiftLeft

	// the partial byte of the current path needs the first shiftLeft bits of the extension path
	buffer[len(cp.value)-1] += path.value[0] >> shiftRight

	// Each byte of the new Path is the first (8-shiftRight) bits of byte i+1 added to the last (8-shiftLeft) bits of byte i
	for i := 0; i < len(path.value)-1; i++ {
		buffer[len(cp.value)+i] += path.value[i]<<shiftLeft + path.value[i+1]>>shiftRight
	}

	// the last byte only has values from byte i, as there is no byte i+1
	buffer[len(buffer)-1] += path.value[len(path.value)-1] << shiftLeft

	return Path{
		value:      *(*string)(unsafe.Pointer(&buffer)),
		length:     cp.length + path.length,
		pathConfig: cp.pathConfig,
	}
}

// Skip returns a new Path that contains the last cp.length-tokensToSkip tokens of the current Path
func (cp Path) Skip(tokensToSkip int) Path {
	if cp.length == tokensToSkip {
		return EmptyPath(cp.branchFactor)
	}
	result := Path{
		value:      cp.value[tokensToSkip/cp.tokensPerByte:],
		length:     cp.length - tokensToSkip,
		pathConfig: cp.pathConfig,
	}

	// if the tokens to skip is a whole number of bytes,
	// the remaining bytes exactly equals the new path
	if tokensToSkip%cp.tokensPerByte == 0 {
		return result
	}

	// tokensToSkip does not remove a whole number of bytes
	// copy the remaining shifted bytes into a new buffer
	buffer := make([]byte, cp.bytesNeeded(result.length))
	shiftRight := cp.bitsToShift(tokensToSkip - 1)
	shiftLeft := Byte - shiftRight

	// Each byte of the new Path is the first (8-shiftRight) bits of byte i+1 added to the last (8-shiftLeft) bits of byte i
	for i := 0; i < len(result.value)-1; i++ {
		buffer[i] += result.value[i]<<shiftLeft + result.value[i+1]>>shiftRight
	}

	// the last byte only has values from byte i, as there is no byte i+1
	buffer[len(buffer)-1] += result.value[len(result.value)-1] << shiftLeft

	result.value = *(*string)(unsafe.Pointer(&buffer))
	return result
}

// Take returns a new Path that contains the first tokensToTake tokens of the current Path
func (cp Path) Take(tokensToTake int) Path {
	result := Path{
		length:     tokensToTake,
		pathConfig: cp.pathConfig,
	}

	if !result.hasPartialByte() {
		result.value = cp.value[:tokensToTake/cp.tokensPerByte]
		return result
	}

	// We need to zero out some bits of the last byte so a simple slice will not work
	// Create a new []byte to store the altered value
	buffer := make([]byte, cp.bytesNeeded(tokensToTake))
	copy(buffer, cp.value)

	// We want to zero out everything to the right of the last token, which is at index [tokensToTake] - 1
	// Mask will be (8-bitsToShift) number of 1's followed by (bitsToShift) number of 0's
	mask := byte(0xFF << cp.bitsToShift(tokensToTake-1))
	buffer[len(buffer)-1] = buffer[len(buffer)-1] & mask

	result.value = *(*string)(unsafe.Pointer(&buffer))
	return result
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
