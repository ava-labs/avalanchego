// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ids

import (
	"fmt"
	"math/big"
	"math/bits"
)

// BigBitSet is a bit-set backed by a big.Int
// Holds values ranging from [0, INT_MAX] (arch-dependent)
// Trying to use negative values will result in a panic.
// This implementation is NOT thread-safe.
type BigBitSet struct {
	bits *big.Int
}

// NewBigBitSet returns a new instance of BigBitSet with [bits] set to 1.
//
// Invariants:
// 1. Negative bits will cause a panic.
// 2. Duplicate bits are allowed but will cause a no-op.
func NewBigBitSet(bits ...int) BigBitSet {
	b := BigBitSet{new(big.Int)}
	for _, bit := range bits {
		b.Add(bit)
	}
	return b
}

// Add sets the [i]'th bit to 1
func (b BigBitSet) Add(i int) {
	b.bits.SetBit(b.bits, i, 1)
}

// Union performs the set union with another set.
// This adds all elements in [other] to [b]
func (b BigBitSet) Union(other BigBitSet) {
	b.bits.Or(b.bits, other.bits)
}

// Intersection performs the set intersection with another set
// This sets [b] to include only elements in both [b] and [other]
func (b BigBitSet) Intersection(other BigBitSet) {
	b.bits.And(b.bits, other.bits)
}

// Difference removes all the elements in [other] from this set
func (b BigBitSet) Difference(other BigBitSet) {
	b.bits.AndNot(b.bits, other.bits)
}

// Remove sets the [i]'th bit to 0
func (b BigBitSet) Remove(i int) {
	b.bits.SetBit(b.bits, i, 0)
}

// Clear empties out the bitset
func (b BigBitSet) Clear() {
	b.bits.SetUint64(0)
}

// Contains returns true if the [i]'th bit is 1, and false otherwise
func (b BigBitSet) Contains(i int) bool {
	return b.bits.Bit(i) == 1
}

// Len returns the bit length of this bitset
func (b BigBitSet) Len() int {
	return b.bits.BitLen()
}

// HammingWeight returns the amount of 1's in the bitset
func (b BigBitSet) HammingWeight() int {
	result := 0
	for _, word := range b.bits.Bits() {
		result += bits.OnesCount(uint(word))
	}
	return result
}

// String returns the hex representation of this bitset
func (b BigBitSet) String() string {
	return fmt.Sprintf("%x", b.bits.Bytes())
}
