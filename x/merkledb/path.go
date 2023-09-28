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
	tokensLength int
	value        string
	pathConfig
}

func emptyPath(bf BranchFactor) Path {
	return Path{
		pathConfig: branchFactorToPathConfig[bf],
	}
}

// NewPath returns [p] as a new path with the given [branchFactor].
// Assumes [branchFactor] is valid.
func NewPath(p []byte, branchFactor BranchFactor) Path {
	pConfig := branchFactorToPathConfig[branchFactor]
	return Path{
		value:        string(p),
		pathConfig:   pConfig,
		tokensLength: len(p) * pConfig.tokensPerByte,
	}
}

// TokensLength returns the number of tokens in [p].
func (p Path) TokensLength() int {
	return p.tokensLength
}

// hasPartialByte returns true iff the path fits into a non-whole number of bytes
func (p Path) hasPartialByte() bool {
	return p.tokensLength%p.tokensPerByte > 0
}

// HasPrefix returns true iff [prefix] is a prefix of [s] or equal to it.
func (p Path) HasPrefix(prefix Path) bool {
	// [prefix] must be shorter than [p] to be a prefix.
	if p.tokensLength < prefix.tokensLength {
		return false
	}

	// The number of tokens in the last byte of [prefix], or zero
	// if [prefix] fits into a whole number of bytes.
	remainderTokens := prefix.tokensLength % p.tokensPerByte
	if remainderTokens == 0 {
		return strings.HasPrefix(p.value, prefix.value)
	}

	// check that the tokens in the partially filled final byte of [prefix] are
	// equal to the tokens in the final byte of [p].
	for i := prefix.tokensLength - remainderTokens; i < prefix.tokensLength; i++ {
		if p.Token(i) != prefix.Token(i) {
			return false
		}
	}

	// Note that this will never be an index OOB because len(prefix.value) > 0.
	// If len(prefix.value) == 0 were true, [remainderTokens] would be 0 so we
	// would have returned above.
	prefixWithoutPartialByte := prefix.value[:len(prefix.value)-1]
	return strings.HasPrefix(p.value, prefixWithoutPartialByte)
}

// HasStrictPrefix returns true iff [prefix] is a prefix of [s] but not equal to it.
func (p Path) HasStrictPrefix(prefix Path) bool {
	return p != prefix && p.HasPrefix(prefix)
}

// Token returns the token at the specified index
// grabs the token's storage byte, shifts it, then masks out any bits from other tokens stored in the same byte
func (p Path) Token(index int) byte {
	return (p.value[index/p.tokensPerByte] >> p.bitsToShift(index)) & p.singleTokenMask
}

// Append returns a new Path that equals the current Path with the passed token appended to the end
func (p Path) Append(token byte) Path {
	buffer := make([]byte, p.bytesNeeded(p.tokensLength+1))
	copy(buffer, p.value)
	buffer[len(buffer)-1] |= token << p.bitsToShift(p.tokensLength)
	return Path{
		value:        *(*string)(unsafe.Pointer(&buffer)),
		tokensLength: p.tokensLength + 1,
		pathConfig:   p.pathConfig,
	}
}

// Greater returns true if current Path is greater than other Path
func (p Path) Greater(other Path) bool {
	return p.value > other.value || (p.value == other.value && p.tokensLength > other.tokensLength)
}

// Less returns true if current Path is less than other Path
func (p Path) Less(other Path) bool {
	return p.value < other.value || (p.value == other.value && p.tokensLength < other.tokensLength)
}

// bitsToShift indicates how many bits to shift the token at the index to get the raw token value.
// varies from (8-[tokenBitSize]) -> (0) when remainder of index varies from (0) -> (p.tokensPerByte-1)
func (p Path) bitsToShift(index int) byte {
	return 8 - (p.tokenBitSize * byte(index%p.tokensPerByte+1))
}

// bytesNeeded returns the number of bytes needed to store the passed number of tokens
func (p Path) bytesNeeded(tokens int) int {
	bytesNeeded := tokens / p.tokensPerByte
	if tokens%p.tokensPerByte > 0 {
		bytesNeeded++
	}
	return bytesNeeded
}

// Extend returns a new Path that equals the passed Path appended to the current Path
func (p Path) Extend(path Path) Path {
	if p.tokensLength == 0 {
		return path
	}
	if path.tokensLength == 0 {
		return p
	}

	totalLength := p.tokensLength + path.tokensLength

	// copy existing value into  the buffer
	buffer := make([]byte, p.bytesNeeded(totalLength))
	copy(buffer, p.value)

	// If the existing value fits into a whole number of bytes,
	// the extension path can be copied directly into the buffer.
	if !p.hasPartialByte() {
		copy(buffer[len(p.value):], path.value)
		return Path{
			value:        *(*string)(unsafe.Pointer(&buffer)),
			tokensLength: totalLength,
			pathConfig:   p.pathConfig,
		}
	}

	// The existing path doesn't fit into a whole number of bytes,
	// figure out how much each byte of the extension path needs to be shifted
	shiftLeft := p.bitsToShift(p.tokensLength - 1)
	// the partial byte of the current path needs the first shiftLeft bits of the extension path
	buffer[len(p.value)-1] |= path.value[0] >> (8 - shiftLeft)

	// copy the rest of the extension path bytes into the buffer, shifted byte shiftLeft bits
	shiftCopy(buffer[len(p.value):], path.value, shiftLeft)

	return Path{
		value:        *(*string)(unsafe.Pointer(&buffer)),
		tokensLength: totalLength,
		pathConfig:   p.pathConfig,
	}
}

func shiftCopy(dst []byte, src string, shift byte) {
	reverseShift := 8 - shift
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
	if p.tokensLength == tokensToSkip {
		return emptyPath(p.branchFactor)
	}
	result := Path{
		value:        p.value[tokensToSkip/p.tokensPerByte:],
		tokensLength: p.tokensLength - tokensToSkip,
		pathConfig:   p.pathConfig,
	}

	// if the tokens to skip is a whole number of bytes,
	// the remaining bytes exactly equals the new path
	if tokensToSkip%p.tokensPerByte == 0 {
		return result
	}

	// tokensToSkip does not remove a whole number of bytes
	// copy the remaining shifted bytes into a new buffer
	buffer := make([]byte, p.bytesNeeded(result.tokensLength))
	bitsSkipped := tokensToSkip * int(p.tokenBitSize)
	bitsRemovedFromFirstRemainingByte := byte(bitsSkipped % 8)
	shiftCopy(buffer, result.value, bitsRemovedFromFirstRemainingByte)

	result.value = *(*string)(unsafe.Pointer(&buffer))
	return result
}

// Take returns a new Path that contains the first tokensToTake tokens of the current Path
func (p Path) Take(tokensToTake int) Path {
	if p.tokensLength == tokensToTake {
		return p
	}

	result := Path{
		tokensLength: tokensToTake,
		pathConfig:   p.pathConfig,
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
