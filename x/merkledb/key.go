// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"
)

var (
	ErrInvalidTokenConfig = errors.New("token configuration must match one of the predefined configurations ")

	BranchFactor2TokenConfig = TokenConfiguration{
		branchFactor:    2,
		tokenBitSize:    1,
		tokensPerByte:   8,
		singleTokenMask: 0b0000_0001,
	}
	BranchFactor4TokenConfig = TokenConfiguration{
		branchFactor:    4,
		tokenBitSize:    2,
		tokensPerByte:   4,
		singleTokenMask: 0b0000_0011,
	}
	BranchFactor16TokenConfig = TokenConfiguration{
		branchFactor:    16,
		tokenBitSize:    4,
		tokensPerByte:   2,
		singleTokenMask: 0b0000_1111,
	}
	BranchFactor256TokenConfig = TokenConfiguration{
		branchFactor:    256,
		tokenBitSize:    8,
		tokensPerByte:   1,
		singleTokenMask: 0b1111_1111,
	}
	validTokenConfigurations = []TokenConfiguration{
		BranchFactor2TokenConfig,
		BranchFactor4TokenConfig,
		BranchFactor16TokenConfig,
		BranchFactor256TokenConfig,
	}
)

type TokenConfiguration struct {
	branchFactor    int
	tokensPerByte   int
	tokenBitSize    int
	singleTokenMask byte
}

func (t TokenConfiguration) Valid() error {
	for _, validConfig := range validTokenConfigurations {
		if validConfig == t {
			return nil
		}
	}
	return fmt.Errorf("%w: %d", ErrInvalidTokenConfig, t)
}

func (t TokenConfiguration) BranchFactor() int {
	return t.branchFactor
}

func (t TokenConfiguration) TokensPerByte() int {
	return t.tokensPerByte
}

func (t TokenConfiguration) TokenBitSize() int {
	return t.tokenBitSize
}

func (t TokenConfiguration) SingleTokenMask() byte {
	return t.singleTokenMask
}

func (t TokenConfiguration) TokenLength(k Key) int {
	return k.bitLength / t.tokenBitSize
}

func (t TokenConfiguration) BitLength(tokens int) int {
	return tokens * t.tokenBitSize
}

type Key struct {
	bitLength int
	value     string
}

// ToKey returns [keyBytes] as a new key with the given [branchFactor].
// Assumes [branchFactor] is valid.
func ToKey(keyBytes []byte) Key {
	return Key{
		value:     byteSliceToString(keyBytes),
		bitLength: len(keyBytes) * 8,
	}
}

func bytesNeeded(bits int) int {
	size := bits / 8
	if bits%8 != 0 {
		size++
	}
	return size
}

// hasPartialByte returns true iff the key fits into a non-whole number of bytes
func (k Key) hasPartialByte() bool {
	return k.bitLength%8 > 0
}

// HasPrefix returns true iff [prefix] is a prefix of [k] or equal to it.
func (k Key) HasPrefix(prefix Key) bool {
	// [prefix] must be shorter than [k] to be a prefix.
	if k.bitLength < prefix.bitLength {
		return false
	}

	// The number of tokens in the last byte of [prefix], or zero
	// if [prefix] fits into a whole number of bytes.
	remainderBitCount := prefix.remainderBitCount()
	if remainderBitCount == 0 {
		return strings.HasPrefix(k.value, prefix.value)
	}

	// check that the tokens in the partially filled final byte of [prefix] are
	// equal to the tokens in the final byte of [k].
	remainderBitsMask := byte(0xFF << (8 - remainderBitCount))
	prefixRemainderTokens := prefix.value[len(prefix.value)-1] & remainderBitsMask
	remainderTokens := k.value[len(prefix.value)-1] & remainderBitsMask

	if prefixRemainderTokens != remainderTokens {
		return false
	}

	// Note that this will never be an index OOB because len(prefix.value) > 0.
	// If len(prefix.value) == 0 were true, [remainderTokens] would be 0 so we
	// would have returned above.
	prefixWithoutPartialByte := prefix.value[:len(prefix.value)-1]
	return strings.HasPrefix(k.value, prefixWithoutPartialByte)
}

// HasStrictPrefix returns true iff [prefix] is a prefix of [k]
// but is not equal to it.
func (k Key) HasStrictPrefix(prefix Key) bool {
	return k != prefix && k.HasPrefix(prefix)
}

func (k Key) remainderBitCount() int {
	return k.bitLength % 8
}

func (k Key) BitLength() int {
	return k.bitLength
}

// Token returns the token at the specified index,
func (k Key) Token(tc TokenConfiguration, index int) byte {
	bitIndex := index * tc.TokenBitSize()
	storageByte := k.value[bitIndex/8]
	// Shift the byte right to get the token to the rightmost position.
	storageByte >>= bitsToShift(tc, bitIndex)
	// Apply a mask to remove any other tokens in the byte.
	return storageByte & (0xFF >> (8 - tc.TokenBitSize()))
}

// Append returns a new Path that equals the current
// Path with [token] appended to the end.
func (k Key) Append(tc TokenConfiguration, token byte) Key {
	buffer := make([]byte, bytesNeeded(k.bitLength+tc.TokenBitSize()))
	k.appendIntoBuffer(tc, buffer, token)
	return Key{
		value:     byteSliceToString(buffer),
		bitLength: k.bitLength + tc.TokenBitSize(),
	}
}

// Greater returns true if current Key is greater than other Key
func (k Key) Greater(other Key) bool {
	return k.value > other.value || (k.value == other.value && k.bitLength > other.bitLength)
}

// Less returns true if current Key is less than other Key
func (k Key) Less(other Key) bool {
	return k.value < other.value || (k.value == other.value && k.bitLength < other.bitLength)
}

// bitsToShift returns the number of bits to right shift a token
// within its storage byte to get it to the rightmost
// position in the byte. Equivalently, this is the number of bits
// to left shift a raw token value to get it to the correct position
// within its storage byte.
// Example with branch factor 16:
// Suppose the token array is
// [0x01, 0x02, 0x03, 0x04]
// The byte representation of this array is
// [0b0001_0010, 0b0011_0100]
// To get the token at index 0 (0b0001) to the rightmost position
// in its storage byte (i.e. to make 0b0001_0010 into 0b0000_0001),
// we need to shift 0b0001_0010 to the right by 4 bits.
// Similarly:
// * Token at index 1 (0b0010) needs to be shifted by 0 bits
// * Token at index 2 (0b0011) needs to be shifted by 4 bits
// * Token at index 3 (0b0100) needs to be shifted by 0 bits
func bitsToShift(tc TokenConfiguration, bitIndex int) byte {
	// [tokenIndex] is the index of the token in the byte.
	// For example, if the branch factor is 16, then each byte contains 2 tokens.
	// The first is at index 0, and the second is at index 1, by this definition.
	startBitIndex := bitIndex % 8
	// The bit within the byte that the token ends at.
	endBitIndex := startBitIndex + tc.TokenBitSize()
	// We want to right shift until [endBitIndex] is at the last index, so return
	// the distance from the end of the byte to the end of the token.
	// Note that 7 is the index of the last bit in a byte.
	return 8 - byte(endBitIndex)
}

func (k Key) AppendExtend(tc TokenConfiguration, token byte, extensionKey Key) Key {
	appendBytes := bytesNeeded(k.bitLength + tc.TokenBitSize())
	totalBitLength := k.bitLength + tc.TokenBitSize() + extensionKey.bitLength
	buffer := make([]byte, bytesNeeded(totalBitLength))
	k.appendIntoBuffer(tc, buffer[:appendBytes], token)

	result := Key{
		value:     byteSliceToString(buffer),
		bitLength: totalBitLength,
	}

	// the extension path will be shifted based on the number of tokens in the partial byte
	bitsRemainder := (k.bitLength + tc.TokenBitSize()) % 8

	extensionBuffer := buffer[appendBytes-1:]
	if extensionKey.bitLength == 0 {
		return result
	}

	// If the existing value fits into a whole number of bytes,
	// the extension path can be copied directly into the buffer.
	if bitsRemainder == 0 {
		copy(extensionBuffer[1:], extensionKey.value)
		return result
	}

	// Fill the partial byte with the first [shift] bits of the extension path
	extensionBuffer[0] |= extensionKey.value[0] >> bitsRemainder

	// copy the rest of the extension path bytes into the buffer,
	// shifted byte shift bits
	shiftCopy(extensionBuffer[1:], extensionKey.value, byte(8-bitsRemainder))

	return result
}

func (k Key) appendIntoBuffer(tc TokenConfiguration, buffer []byte, token byte) {
	copy(buffer, k.value)
	buffer[len(buffer)-1] |= token << bitsToShift(tc, k.bitLength)
}

// Treats [src] as a bit array and copies it into [dst] shifted by [shift] bits.
// For example, if [src] is [0b0000_0001, 0b0000_0010] and [shift] is 4,
// we copy [0b0001_0000, 0b0010_0000] into [dst].
// Assumes len(dst) >= len(src)-1.
// If len(dst) == len(src)-1 the last byte of [src] is only partially copied
// (i.e. the rightmost bits are not copied).
func shiftCopy(dst []byte, src string, shift byte) {
	i := 0
	for ; i < len(src)-1; i++ {
		dst[i] = src[i]<<shift | src[i+1]>>(8-shift)
	}

	if i < len(dst) {
		// the last byte only has values from byte i, as there is no byte i+1
		dst[i] = src[i] << shift
	}
}

// Skip returns a new Key that contains the last
// k.length-tokensToSkip tokens of [k].
func (k Key) Skip(tc TokenConfiguration, tokensToSkip int) Key {
	bitsToSkip := tc.BitLength(tokensToSkip)
	if k.bitLength <= bitsToSkip {
		return Key{}
	}
	result := Key{
		value:     k.value[bitsToSkip/8:],
		bitLength: k.bitLength - bitsToSkip,
	}

	// if the tokens to skip is a whole number of bytes,
	// the remaining bytes exactly equals the new key.
	if bitsToSkip%8 == 0 {
		return result
	}

	// tokensToSkip does not remove a whole number of bytes.
	// copy the remaining shifted bytes into a new buffer.
	buffer := make([]byte, bytesNeeded(result.bitLength))
	bitsRemovedFromFirstRemainingByte := byte(bitsToSkip % 8)
	shiftCopy(buffer, result.value, bitsRemovedFromFirstRemainingByte)

	result.value = byteSliceToString(buffer)
	return result
}

// Take returns a new Key that contains the first tokensToTake tokens of the current Key
func (k Key) Take(tc TokenConfiguration, tokensToTake int) Key {
	bitsToTake := tc.BitLength(tokensToTake)
	if k.bitLength <= bitsToTake {
		return k
	}

	result := Key{
		bitLength: bitsToTake,
	}

	remainderBits := result.remainderBitCount()
	if remainderBits == 0 {
		result.value = k.value[:bitsToTake/8]
		return result
	}

	// We need to zero out some bits of the last byte so a simple slice will not work
	// Create a new []byte to store the altered value
	buffer := make([]byte, bytesNeeded(bitsToTake))
	copy(buffer, k.value)

	// We want to zero out everything to the right of the last token, which is at index [tokensToTake] - 1
	// Mask will be (8-remainderBits) number of 1's followed by (remainderBits) number of 0's
	buffer[len(buffer)-1] &= byte(0xFF << (8 - remainderBits))

	result.value = byteSliceToString(buffer)
	return result
}

// Bytes returns the raw bytes of the Key
// Invariant: The returned value must not be modified.
func (k Key) Bytes() []byte {
	// avoid copying during the conversion
	// "safe" because we never edit the value, only used as DB key
	return stringToByteSlice(k.value)
}

// iteratedHasPrefix checks if the provided prefix path is a prefix of the current path after having skipped [skipTokens] tokens first
// this has better performance than constructing the actual path via Skip() then calling HasPrefix because it avoids the []byte allocation
func (k Key) iteratedHasPrefix(tc TokenConfiguration, skipTokens int, prefix Key) bool {
	if k.bitLength-tc.BitLength(skipTokens) < prefix.bitLength {
		return false
	}
	for i := 0; i < tc.TokenLength(prefix); i++ {
		if k.Token(tc, skipTokens+i) != prefix.Token(tc, i) {
			return false
		}
	}
	return true
}

// byteSliceToString converts the []byte to a string
// Invariant: The input []byte must not be modified.
func byteSliceToString(bs []byte) string {
	// avoid copying during the conversion
	// "safe" because we never edit the []byte, and it is never returned by any functions except Bytes()
	return unsafe.String(unsafe.SliceData(bs), len(bs))
}

// stringToByteSlice converts the string to a []byte
// Invariant: The output []byte must not be modified.
func stringToByteSlice(value string) []byte {
	// avoid copying during the conversion
	// "safe" because we never edit the []byte
	return unsafe.Slice(unsafe.StringData(value), len(value))
}
