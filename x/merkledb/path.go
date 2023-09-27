// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unsafe"
)

// Used in Path bit operations.
const eight byte = 8

var (
	errInvalidBranchFactor = errors.New("invalid branch factor")

	branchFactorToPathConfig = map[BranchFactor]pathConfig{
		BranchFactor2: {
			branchFactor:    BranchFactor2,
			tokenBitSize:    1,
			tokensPerByte:   8,
			singleTokenMask: 0b0000_0001,
		},
		BranchFactor4: {
			branchFactor:    BranchFactor4,
			tokenBitSize:    2,
			tokensPerByte:   4,
			singleTokenMask: 0b0000_0011,
		},
		BranchFactor16: {
			branchFactor:    BranchFactor16,
			tokenBitSize:    4,
			tokensPerByte:   2,
			singleTokenMask: 0b0000_1111,
		},
		BranchFactor256: {
			branchFactor:    BranchFactor256,
			tokenBitSize:    8,
			tokensPerByte:   1,
			singleTokenMask: 0b1111_1111,
		},
	}
)

type BranchFactor int

const (
	BranchFactor2   BranchFactor = 2
	BranchFactor4   BranchFactor = 4
	BranchFactor16  BranchFactor = 16
	BranchFactor256 BranchFactor = 256
)

func (f BranchFactor) Valid() error {
	if _, ok := branchFactorToPathConfig[f]; ok {
		return nil
	}
	return fmt.Errorf("%w: %d", errInvalidBranchFactor, f)
}

type pathConfig struct {
	branchFactor    BranchFactor
	tokensPerByte   int
	tokenBitSize    byte
	singleTokenMask byte
}

type Path struct {
	length int
	value  string
	pathConfig
}

func emptyPath(bf BranchFactor) Path {
	return Path{
		pathConfig: branchFactorToPathConfig[bf],
	}
}

// NewPath creates a new path based on the []bytes and branch factor
// assumes [branchFactor] is valid
func NewPath(p []byte, branchFactor BranchFactor) Path {
	pConfig := branchFactorToPathConfig[branchFactor]
	return Path{
		value:      string(p),
		pathConfig: pConfig,
		length:     len(p) * pConfig.tokensPerByte,
	}
}

// Length returns the number of tokens in the current Path
func (p Path) Length() int {
	return p.length
}

// hasPartialByte returns true iff the path fits into a non-whole number of bytes
func (p Path) hasPartialByte() bool {
	return p.length%p.tokensPerByte > 0
}

// HasPrefix returns true iff [prefix] is a prefix of [s] or equal to it.
func (p Path) HasPrefix(prefix Path) bool {
	// if the current path is shorter than the prefix path, then it cannot be a prefix
	if p.length < prefix.length || len(p.value) < len(prefix.value) {
		return false
	}

	remainderTokens := prefix.length % p.tokensPerByte
	if remainderTokens == 0 {
		return strings.HasPrefix(p.value, prefix.value)
	}

	// check that the tokens that were in the partially filled prefix byte are equal to the tokens in the current path
	for i := prefix.length - remainderTokens; i < prefix.length; i++ {
		if p.Token(i) != prefix.Token(i) {
			return false
		}
	}

	// the prefix contains a partially filled byte so grab the length of whole bytes
	wholeBytesLength := len(prefix.value) - 1

	// the input was valid and the whole bytes of the prefix are a prefix of the current path
	return wholeBytesLength >= 0 && strings.HasPrefix(p.value, prefix.value[:wholeBytesLength])
}

// HasStrictPrefix Returns true iff [prefix] is a prefix of [s] but not equal to it.
func (p Path) HasStrictPrefix(prefix Path) bool {
	return p.HasPrefix(prefix) && p != prefix
}

// Token returns the token at the specified index
// grabs the token's storage byte, shifts it, then masks out any bits from other tokens stored in the same byte
func (p Path) Token(index int) byte {
	return (p.value[index/p.tokensPerByte] >> p.bitsToShift(index)) & p.singleTokenMask
}

// Append returns a new Path that equals the current Path with the passed token appended to the end
func (p Path) Append(token byte) Path {
	buffer := make([]byte, p.bytesNeeded(p.length+1))
	copy(buffer, p.value)
	buffer[len(buffer)-1] |= token << p.bitsToShift(p.length)
	return Path{
		value:      *(*string)(unsafe.Pointer(&buffer)),
		length:     p.length + 1,
		pathConfig: p.pathConfig,
	}
}

// Greater returns true if current Path is greater than other Path
func (p Path) Greater(other Path) bool {
	return p.value > other.value || (p.value == other.value && p.length > other.length)
}

// Less returns true if current Path is less than other Path
func (p Path) Less(other Path) bool {
	return p.value < other.value || (p.value == other.value && p.length < other.length)
}

// bitsToShift indicates how many bits to shift the token at the index to get the raw token value
// varies from (8-[tokenBitSize]) -> (0) when remainder of index varies from (0) -> (p.tokensPerByte-1)
func (p Path) bitsToShift(index int) byte {
	return eight - (p.tokenBitSize * byte(index%p.tokensPerByte+1))
}

// bytesNeeded returns the number of bytes needed to store the passed number of tokens
func (p Path) bytesNeeded(tokens int) int {
	// (p.tokensPerByte - 1) ensures that it rounds up
	// acts a Ceiling
	return (tokens + p.tokensPerByte - 1) / p.tokensPerByte
}

// Extend returns a new Path that equals the passed Path appended to the current Path
func (p Path) Extend(path Path) Path {
	if p.length == 0 {
		return path
	}
	if path.length == 0 {
		return p
	}

	totalLength := p.length + path.length

	// copy existing value into  the buffer
	buffer := make([]byte, p.bytesNeeded(totalLength))
	copy(buffer, p.value)

	// If the existing value fits into a whole number of bytes,
	// the extension path can be copied directly into the buffer.
	if !p.hasPartialByte() {
		copy(buffer[len(p.value):], path.value)
		return Path{
			value:      *(*string)(unsafe.Pointer(&buffer)),
			length:     totalLength,
			pathConfig: p.pathConfig,
		}
	}

	// The existing path doesn't fit into a whole number of bytes,
	// figure out how much each byte of the extension path needs to be shifted
	shiftLeft := p.bitsToShift(p.length - 1)
	// the partial byte of the current path needs the first shiftLeft bits of the extension path
	buffer[len(p.value)-1] |= path.value[0] >> (eight - shiftLeft)

	// copy the rest of the extension path bytes into the buffer, shifted byte shiftLeft bits
	shiftCopy(buffer[len(p.value):], path.value, shiftLeft)

	return Path{
		value:      *(*string)(unsafe.Pointer(&buffer)),
		length:     totalLength,
		pathConfig: p.pathConfig,
	}
}

func shiftCopy(dst []byte, src string, shift byte) {
	reverseShift := eight - shift
	i := 0
	for ; i < len(src)-1; i++ {
		dst[i] = src[i]<<shift + src[i+1]>>reverseShift
	}

	if i < len(dst) {
		// the last byte only has values from byte i, as there is no byte i+1
		dst[i] = src[i] << shift
	}
}

// Skip returns a new Path that contains the last p.length-tokensToSkip tokens of the current Path
func (p Path) Skip(tokensToSkip int) Path {
	if p.length == tokensToSkip {
		return emptyPath(p.branchFactor)
	}
	result := Path{
		value:      p.value[tokensToSkip/p.tokensPerByte:],
		length:     p.length - tokensToSkip,
		pathConfig: p.pathConfig,
	}

	// if the tokens to skip is a whole number of bytes,
	// the remaining bytes exactly equals the new path
	if tokensToSkip%p.tokensPerByte == 0 {
		return result
	}

	// tokensToSkip does not remove a whole number of bytes
	// copy the remaining shifted bytes into a new buffer
	buffer := make([]byte, p.bytesNeeded(result.length))
	bitsSkipped := tokensToSkip * int(p.tokenBitSize)
	bitsRemovedFromFirstRemainingByte := byte(bitsSkipped % 8)
	shiftCopy(buffer, result.value, bitsRemovedFromFirstRemainingByte)

	result.value = *(*string)(unsafe.Pointer(&buffer))
	return result
}

// Take returns a new Path that contains the first tokensToTake tokens of the current Path
func (p Path) Take(tokensToTake int) Path {
	result := Path{
		length:     tokensToTake,
		pathConfig: p.pathConfig,
	}

	if !result.hasPartialByte() {
		result.value = p.value[:tokensToTake/p.tokensPerByte]
		return result
	}

	// We need to zero out some bits of the last byte so a simple slice will not work
	// Create a new []byte to store the altered value
	buffer := make([]byte, p.bytesNeeded(tokensToTake))
	copy(buffer, p.value)

	// We want to zero out everything to the right of the last token, which is at index [tokensToTake] - 1
	// Mask will be (8-bitsToShift) number of 1's followed by (bitsToShift) number of 0's
	mask := byte(0xFF << p.bitsToShift(tokensToTake-1))
	buffer[len(buffer)-1] = buffer[len(buffer)-1] & mask

	result.value = *(*string)(unsafe.Pointer(&buffer))
	return result
}

// Bytes returns the raw bytes of the Path
// Invariant: The returned value must not be modified.
func (p Path) Bytes() []byte {
	// avoid copying during the conversion
	// "safe" because we never edit the value, only used as DB key
	buf := *(*[]byte)(unsafe.Pointer(&p.value))
	(*reflect.SliceHeader)(unsafe.Pointer(&buf)).Cap = len(p.value)
	return buf
}
