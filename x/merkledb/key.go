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
	errInvalidBranchFactor = errors.New("invalid branch factor")

	branchFactorToTokenConfig = map[BranchFactor]tokenConfig{
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
	if _, ok := branchFactorToTokenConfig[f]; ok {
		return nil
	}
	return fmt.Errorf("%w: %d", errInvalidBranchFactor, f)
}

type tokenConfig struct {
	branchFactor    BranchFactor
	tokensPerByte   int
	tokenBitSize    byte
	singleTokenMask byte
}

type Key struct {
	tokenLength int
	value       string
	tokenConfig
}

func emptyKey(bf BranchFactor) Key {
	return Key{
		tokenConfig: branchFactorToTokenConfig[bf],
	}
}

// ToKey returns [keyBytes] as a new key with the given [branchFactor].
// Assumes [branchFactor] is valid.
func ToKey(keyBytes []byte, branchFactor BranchFactor) Key {
	tc := branchFactorToTokenConfig[branchFactor]
	return Key{
		value:       byteSliceToString(keyBytes),
		tokenConfig: tc,
		tokenLength: len(keyBytes) * tc.tokensPerByte,
	}
}

// TokensLength returns the number of tokens in [k].
func (k Key) TokensLength() int {
	return k.tokenLength
}

// hasPartialByte returns true iff the key fits into a non-whole number of bytes
func (k Key) hasPartialByte() bool {
	return k.tokenLength%k.tokensPerByte > 0
}

// HasPrefix returns true iff [prefix] is a prefix of [k] or equal to it.
func (k Key) HasPrefix(prefix Key) bool {
	// [prefix] must be shorter than [k] to be a prefix.
	if k.tokenLength < prefix.tokenLength {
		return false
	}

	// The number of tokens in the last byte of [prefix], or zero
	// if [prefix] fits into a whole number of bytes.
	remainderTokensCount := prefix.tokenLength % k.tokensPerByte
	if remainderTokensCount == 0 {
		return strings.HasPrefix(k.value, prefix.value)
	}

	// check that the tokens in the partially filled final byte of [prefix] are
	// equal to the tokens in the final byte of [k].
	remainderBitsMask := byte(0xFF << (8 - remainderTokensCount*int(k.tokenBitSize)))
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

// Token returns the token at the specified index,
func (k Key) Token(index int) byte {
	// Find the index in [k.value] of the byte containing the token at [index].
	storageByteIndex := index / k.tokensPerByte
	storageByte := k.value[storageByteIndex]
	// Shift the byte right to get the token to the rightmost position.
	storageByte >>= k.bitsToShift(index)
	// Apply a mask to remove any other tokens in the byte.
	return storageByte & k.singleTokenMask
}

// Append returns a new Path that equals the current
// Path with [token] appended to the end.
func (k Key) Append(token byte) Key {
	buffer := make([]byte, k.bytesNeeded(k.tokenLength+1))
	k.appendIntoBuffer(buffer, token)
	return Key{
		value:       byteSliceToString(buffer),
		tokenLength: k.tokenLength + 1,
		tokenConfig: k.tokenConfig,
	}
}

// Greater returns true if current Key is greater than other Key
func (k Key) Greater(other Key) bool {
	return k.value > other.value || (k.value == other.value && k.tokenLength > other.tokenLength)
}

// Less returns true if current Key is less than other Key
func (k Key) Less(other Key) bool {
	return k.value < other.value || (k.value == other.value && k.tokenLength < other.tokenLength)
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
func (k Key) bitsToShift(index int) byte {
	// [tokenIndex] is the index of the token in the byte.
	// For example, if the branch factor is 16, then each byte contains 2 tokens.
	// The first is at index 0, and the second is at index 1, by this definition.
	tokenIndex := index % k.tokensPerByte
	// The bit within the byte that the token starts at.
	startBitIndex := k.tokenBitSize * byte(tokenIndex)
	// The bit within the byte that the token ends at.
	endBitIndex := startBitIndex + k.tokenBitSize - 1
	// We want to right shift until [endBitIndex] is at the last index, so return
	// the distance from the end of the byte to the end of the token.
	// Note that 7 is the index of the last bit in a byte.
	return 7 - endBitIndex
}

// bytesNeeded returns the number of bytes needed to store the passed number of
// tokens.
//
// Invariant: [tokens] is a non-negative, but otherwise untrusted, input and
// this method must never overflow.
func (k Key) bytesNeeded(tokens int) int {
	size := tokens / k.tokensPerByte
	if tokens%k.tokensPerByte != 0 {
		size++
	}
	return size
}

func (k Key) AppendExtend(token byte, extensionKey Key) Key {
	appendBytes := k.bytesNeeded(k.tokenLength + 1)
	totalLength := k.tokenLength + 1 + extensionKey.tokenLength
	buffer := make([]byte, k.bytesNeeded(totalLength))
	k.appendIntoBuffer(buffer[:appendBytes], token)

	// the extension path will be shifted based on the number of tokens in the partial byte
	tokenRemainder := (k.tokenLength + 1) % k.tokensPerByte
	result := Key{
		value:       byteSliceToString(buffer),
		tokenLength: totalLength,
		tokenConfig: k.tokenConfig,
	}

	extensionBuffer := buffer[appendBytes-1:]
	if extensionKey.tokenLength == 0 {
		return result
	}

	// If the existing value fits into a whole number of bytes,
	// the extension path can be copied directly into the buffer.
	if tokenRemainder == 0 {
		copy(extensionBuffer[1:], extensionKey.value)
		return result
	}

	// The existing path doesn't fit into a whole number of bytes.
	// Figure out how many bits to shift.
	shift := extensionKey.bitsToShift(tokenRemainder - 1)
	// Fill the partial byte with the first [shift] bits of the extension path
	extensionBuffer[0] |= extensionKey.value[0] >> (8 - shift)

	// copy the rest of the extension path bytes into the buffer,
	// shifted byte shift bits
	shiftCopy(extensionBuffer[1:], extensionKey.value, shift)

	return result
}

func (k Key) appendIntoBuffer(buffer []byte, token byte) {
	copy(buffer, k.value)

	// Shift [token] to the left such that it's at the correct
	// index within its storage byte, then OR it with its storage
	// byte to write the token into the byte.
	buffer[len(buffer)-1] |= token << k.bitsToShift(k.tokenLength)
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
func (k Key) Skip(tokensToSkip int) Key {
	if k.tokenLength == tokensToSkip {
		return emptyKey(k.branchFactor)
	}
	result := Key{
		value:       k.value[tokensToSkip/k.tokensPerByte:],
		tokenLength: k.tokenLength - tokensToSkip,
		tokenConfig: k.tokenConfig,
	}

	// if the tokens to skip is a whole number of bytes,
	// the remaining bytes exactly equals the new key.
	if tokensToSkip%k.tokensPerByte == 0 {
		return result
	}

	// tokensToSkip does not remove a whole number of bytes.
	// copy the remaining shifted bytes into a new buffer.
	buffer := make([]byte, k.bytesNeeded(result.tokenLength))
	bitsSkipped := tokensToSkip * int(k.tokenBitSize)
	bitsRemovedFromFirstRemainingByte := byte(bitsSkipped % 8)
	shiftCopy(buffer, result.value, bitsRemovedFromFirstRemainingByte)

	result.value = byteSliceToString(buffer)
	return result
}

// Take returns a new Key that contains the first tokensToTake tokens of the current Key
func (k Key) Take(tokensToTake int) Key {
	if k.tokenLength <= tokensToTake {
		return k
	}

	result := Key{
		tokenLength: tokensToTake,
		tokenConfig: k.tokenConfig,
	}

	if !result.hasPartialByte() {
		result.value = k.value[:tokensToTake/k.tokensPerByte]
		return result
	}

	// We need to zero out some bits of the last byte so a simple slice will not work
	// Create a new []byte to store the altered value
	buffer := make([]byte, k.bytesNeeded(tokensToTake))
	copy(buffer, k.value)

	// We want to zero out everything to the right of the last token, which is at index [tokensToTake] - 1
	// Mask will be (8-bitsToShift) number of 1's followed by (bitsToShift) number of 0's
	mask := byte(0xFF << k.bitsToShift(tokensToTake-1))
	buffer[len(buffer)-1] &= mask

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
func (k Key) iteratedHasPrefix(skipTokens int, prefix Key) bool {
	if k.tokenLength-skipTokens < prefix.tokenLength {
		return false
	}
	for i := 0; i < prefix.tokenLength; i++ {
		if k.Token(skipTokens+i) != prefix.Token(i) {
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
