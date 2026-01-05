// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package set

import (
	"fmt"
	"math/bits"
)

// Bits64 is a set that can contain uints in the range [0, 64). All functions
// are O(1). The zero value is the empty set.
type Bits64 uint64

// Add [i] to the set of ints
func (b *Bits64) Add(i uint) {
	*b |= 1 << i
}

// Union adds all the elements in [s] to this set
func (b *Bits64) Union(s Bits64) {
	*b |= s
}

// Intersection takes the intersection of [s] with this set
func (b *Bits64) Intersection(s Bits64) {
	*b &= s
}

// Difference removes all the elements in [s] from this set
func (b *Bits64) Difference(s Bits64) {
	*b &^= s
}

// Remove [i] from the set of ints
func (b *Bits64) Remove(i uint) {
	*b &^= 1 << i
}

// Clear removes all elements from this set
func (b *Bits64) Clear() {
	*b = 0
}

// Contains returns true if [i] was previously added to this set
func (b Bits64) Contains(i uint) bool {
	return b&(1<<i) != 0
}

// Len returns the number of elements in this set
func (b Bits64) Len() int {
	return bits.OnesCount64(uint64(b))
}

func (b Bits64) String() string {
	return fmt.Sprintf("%016x", uint64(b))
}
