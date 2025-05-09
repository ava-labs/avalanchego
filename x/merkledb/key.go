// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unsafe"

	"golang.org/x/exp/maps"
)

var (
	ErrInvalidBranchFactor = errors.New("branch factor must match one of the predefined branch factors")

	BranchFactorToTokenSize = map[BranchFactor]int{
		BranchFactor2:   1,
		BranchFactor4:   2,
		BranchFactor16:  4,
		BranchFactor256: 8,
	}

	tokenSizeToBranchFactor = map[int]BranchFactor{
		1: BranchFactor2,
		2: BranchFactor4,
		4: BranchFactor16,
		8: BranchFactor256,
	}

	validTokenSizes = maps.Keys(tokenSizeToBranchFactor)

	validBranchFactors = []BranchFactor{
		BranchFactor2,
		BranchFactor4,
		BranchFactor16,
		BranchFactor256,
	}
)

type BranchFactor int

const (
	BranchFactor2   = BranchFactor(2)
	BranchFactor4   = BranchFactor(4)
	BranchFactor16  = BranchFactor(16)
	BranchFactor256 = BranchFactor(256)

	BranchFactorLargest = BranchFactor256
)

// Valid checks if BranchFactor [b] is one of the predefined valid options for BranchFactor
func (b BranchFactor) Valid() error {
	if slices.Contains(validBranchFactors, b) {
		return nil
	}
	return fmt.Errorf("%w: %d", ErrInvalidBranchFactor, b)
}

// ToToken creates a key version of the passed byte with bit length equal to tokenSize
func ToToken(val byte, tokenSize int) Key {
	return Key{
		value:  string([]byte{val << dualBitIndex(tokenSize)}),
		length: tokenSize,
	}
}

// Token returns the token at the specified index,
// Assumes that bitIndex + tokenSize doesn't cross a byte boundary
func (k Key) Token(bitIndex int, tokenSize int) byte {
	storageByte := k.value[bitIndex/8]
	// Shift the byte right to get the last bit to the rightmost position.
	storageByte >>= dualBitIndex((bitIndex + tokenSize) % 8)
	// Apply a mask to remove any other bits in the byte.
	return storageByte & (0xFF >> dualBitIndex(tokenSize))
}

// iteratedHasPrefix checks if the provided prefix key is a prefix of the current key starting after the [bitsOffset]th bit
// this has better performance than constructing the actual key via Skip() then calling HasPrefix because it avoids an allocation
func (k Key) iteratedHasPrefix(prefix Key, bitsOffset int, tokenSize int) bool {
	if k.length-bitsOffset < prefix.length {
		return false
	}
	for i := 0; i < prefix.length; i += tokenSize {
		if k.Token(bitsOffset+i, tokenSize) != prefix.Token(i, tokenSize) {
			return false
		}
	}
	return true
}

type Key struct {
	// The number of bits in the key.
	length int
	// The string representation of the key
	value string
}

// ToKey returns [keyBytes] as a new key
// Assumes all bits of the keyBytes are part of the Key, call Key.Take if that is not the case
// Creates a copy of [keyBytes], so keyBytes are safe to edit after the call
func ToKey(keyBytes []byte) Key {
	return toKey(slices.Clone(keyBytes))
}

// toKey returns [keyBytes] as a new key
// Assumes all bits of the keyBytes are part of the Key, call Key.Take if that is not the case
// Caller must not modify [keyBytes] after this call.
func toKey(keyBytes []byte) Key {
	return Key{
		value:  byteSliceToString(keyBytes),
		length: len(keyBytes) * 8,
	}
}

// hasPartialByte returns true iff the key fits into a non-whole number of bytes
func (k Key) hasPartialByte() bool {
	return k.length%8 > 0
}

// HasPrefix returns true iff [prefix] is a prefix of [k] or equal to it.
func (k Key) HasPrefix(prefix Key) bool {
	// [prefix] must be shorter than [k] to be a prefix.
	if k.length < prefix.length {
		return false
	}

	// The number of tokens in the last byte of [prefix], or zero
	// if [prefix] fits into a whole number of bytes.
	remainderBitCount := prefix.length % 8
	if remainderBitCount == 0 {
		return strings.HasPrefix(k.value, prefix.value)
	}

	// check that the tokens in the partially filled final byte of [prefix] are
	// equal to the tokens in the final byte of [k].
	remainderBitsMask := byte(0xFF >> remainderBitCount)
	prefixRemainderTokens := prefix.value[len(prefix.value)-1] | remainderBitsMask
	remainderTokens := k.value[len(prefix.value)-1] | remainderBitsMask

	if prefixRemainderTokens != remainderTokens {
		return false
	}

	// Note that this will never be an index OOB because len(prefix.value) > 0.
	// If len(prefix.value) == 0 were true, [remainderTokens] would be 0, so we
	// would have returned above.
	prefixWithoutPartialByte := prefix.value[:len(prefix.value)-1]
	return strings.HasPrefix(k.value, prefixWithoutPartialByte)
}

// HasStrictPrefix returns true iff [prefix] is a prefix of [k]
// but is not equal to it.
func (k Key) HasStrictPrefix(prefix Key) bool {
	return k != prefix && k.HasPrefix(prefix)
}

// Length returns the number of bits in the Key
func (k Key) Length() int {
	return k.length
}

// Greater returns true if current Key is greater than other Key
func (k Key) Greater(other Key) bool {
	return k.Compare(other) == 1
}

// Less will return true if current Key is less than other Key
func (k Key) Less(other Key) bool {
	return k.Compare(other) == -1
}

func (k Key) Compare(other Key) int {
	if valueCmp := cmp.Compare(k.value, other.value); valueCmp != 0 {
		return valueCmp
	}
	return cmp.Compare(k.length, other.length)
}

// Extend returns a new Key that is the in-order aggregation of Key [k] with [keys]
func (k Key) Extend(keys ...Key) Key {
	totalBitLength := k.length
	for _, key := range keys {
		totalBitLength += key.length
	}
	buffer := make([]byte, bytesNeeded(totalBitLength))
	copy(buffer, k.value)
	currentTotal := k.length
	for _, key := range keys {
		extendIntoBuffer(buffer, key, currentTotal)
		currentTotal += key.length
	}

	return Key{
		value:  byteSliceToString(buffer),
		length: totalBitLength,
	}
}

func extendIntoBuffer(buffer []byte, val Key, bitsOffset int) {
	if val.length == 0 {
		return
	}
	bytesOffset := bytesNeeded(bitsOffset)
	bitsRemainder := bitsOffset % 8
	if bitsRemainder == 0 {
		copy(buffer[bytesOffset:], val.value)
		return
	}

	// Fill the partial byte with the first [shift] bits of the extension path
	buffer[bytesOffset-1] |= val.value[0] >> bitsRemainder

	// copy the rest of the extension path bytes into the buffer,
	// shifted byte shift bits
	shiftCopy(buffer[bytesOffset:], val.value, dualBitIndex(bitsRemainder))
}

// dualBitIndex gets the dual of the bit index
// ex: in a byte, the bit 5 from the right is the same as the bit 3 from the left
func dualBitIndex(shift int) int {
	return (8 - shift) % 8
}

// Treats [src] as a bit array and copies it into [dst] shifted by [shift] bits.
// For example, if [src] is [0b0000_0001, 0b0000_0010] and [shift] is 4,
// we copy [0b0001_0000, 0b0010_0000] into [dst].
// Assumes len(dst) >= len(src)-1.
// If len(dst) == len(src)-1 the last byte of [src] is only partially copied
// (i.e. the rightmost bits are not copied).
func shiftCopy(dst []byte, src string, shift int) {
	i := 0
	dualShift := dualBitIndex(shift)
	for ; i < len(src)-1; i++ {
		dst[i] = src[i]<<shift | src[i+1]>>dualShift
	}

	if i < len(dst) {
		// the last byte only has values from byte i, as there is no byte i+1
		dst[i] = src[i] << shift
	}
}

// Skip returns a new Key that contains the last
// k.length-bitsToSkip bits of [k].
func (k Key) Skip(bitsToSkip int) Key {
	if k.length <= bitsToSkip {
		return Key{}
	}
	result := Key{
		value:  k.value[bitsToSkip/8:],
		length: k.length - bitsToSkip,
	}

	// if the tokens to skip is a whole number of bytes,
	// the remaining bytes exactly equals the new key.
	if bitsToSkip%8 == 0 {
		return result
	}

	// bitsToSkip does not remove a whole number of bytes.
	// copy the remaining shifted bytes into a new buffer.
	buffer := make([]byte, bytesNeeded(result.length))
	bitsRemovedFromFirstRemainingByte := bitsToSkip % 8
	shiftCopy(buffer, result.value, bitsRemovedFromFirstRemainingByte)

	result.value = byteSliceToString(buffer)
	return result
}

// Take returns a new Key that contains the first bitsToTake bits of the current Key
func (k Key) Take(bitsToTake int) Key {
	if k.length <= bitsToTake {
		return k
	}

	result := Key{
		length: bitsToTake,
	}

	remainderBits := result.length % 8
	if remainderBits == 0 {
		result.value = k.value[:bitsToTake/8]
		return result
	}

	// We need to zero out some bits of the last byte so a simple slice will not work
	// Create a new []byte to store the altered value
	buffer := make([]byte, bytesNeeded(bitsToTake))
	copy(buffer, k.value)

	// We want to zero out everything to the right of the last token, which is at index bitsToTake-1
	// Mask will be (8-remainderBits) number of 1's followed by (remainderBits) number of 0's
	buffer[len(buffer)-1] &= byte(0xFF << dualBitIndex(remainderBits))

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

// Returns the number of bytes needed to store [bits] bits.
func bytesNeeded(bits int) int {
	size := bits / 8
	if bits%8 != 0 {
		size++
	}
	return size
}
