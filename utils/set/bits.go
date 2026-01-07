// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"encoding/hex"
	"math/big"
	"math/bits"
)

// Bits is a bit-set backed by a big.Int
// Holds values ranging from [0, INT_MAX] (arch-dependent)
// Trying to use negative values will result in a panic.
// This implementation is NOT thread-safe.
type Bits struct {
	bits *big.Int
}

// NewBits returns a new instance of Bits with [bits] set to 1.
//
// Invariants:
// 1. Negative bits will cause a panic.
// 2. Duplicate bits are allowed but will cause a no-op.
func NewBits(bits ...int) Bits {
	b := Bits{new(big.Int)}
	for _, bit := range bits {
		b.Add(bit)
	}
	return b
}

// Add sets the [i]'th bit to 1
func (b Bits) Add(i int) {
	b.bits.SetBit(b.bits, i, 1)
}

// Union performs the set union with another set.
// This adds all elements in [other] to [b]
func (b Bits) Union(other Bits) {
	b.bits.Or(b.bits, other.bits)
}

// Intersection performs the set intersection with another set
// This sets [b] to include only elements in both [b] and [other]
func (b Bits) Intersection(other Bits) {
	b.bits.And(b.bits, other.bits)
}

// Difference removes all the elements in [other] from this set
func (b Bits) Difference(other Bits) {
	b.bits.AndNot(b.bits, other.bits)
}

// Remove sets the [i]'th bit to 0
func (b Bits) Remove(i int) {
	b.bits.SetBit(b.bits, i, 0)
}

// Clear empties out the bitset
func (b Bits) Clear() {
	b.bits.SetUint64(0)
}

// Contains returns true if the [i]'th bit is 1, and false otherwise
func (b Bits) Contains(i int) bool {
	return b.bits.Bit(i) == 1
}

// BitLen returns the bit length of this bitset
func (b Bits) BitLen() int {
	return b.bits.BitLen()
}

// Len returns the amount of 1's in the bitset
//
// This is typically referred to as the "Hamming Weight"
// of a set of bits.
func (b Bits) Len() int {
	result := 0
	for _, word := range b.bits.Bits() {
		result += bits.OnesCount(uint(word))
	}
	return result
}

// Returns the byte representation of this bitset
func (b Bits) Bytes() []byte {
	return b.bits.Bytes()
}

// Inverse of Bits.Bytes()
func BitsFromBytes(bytes []byte) Bits {
	return Bits{
		bits: new(big.Int).SetBytes(bytes),
	}
}

// String returns the hex representation of this bitset
func (b Bits) String() string {
	return hex.EncodeToString(b.bits.Bytes())
}
