// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bag

import (
	"fmt"
	"strings"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Maps a key to a bitset.
type UniqueBag[T comparable] map[T]set.Bits64

func (b *UniqueBag[T]) init() {
	if *b == nil {
		*b = make(map[T]set.Bits64, minBagSize)
	}
}

// Adds [n] to the bitset associated with each key in [keys].
func (b *UniqueBag[T]) Add(n uint, keys ...T) {
	var bs set.Bits64
	bs.Add(n)

	for _, key := range keys {
		b.UnionSet(key, bs)
	}
}

// Unions [set] with the bitset associated with [key].
func (b *UniqueBag[T]) UnionSet(key T, set set.Bits64) {
	b.init()

	previousSet := (*b)[key]
	previousSet.Union(set)
	(*b)[key] = previousSet
}

// Removes each element of [set] from the bitset associated with [key].
func (b *UniqueBag[T]) DifferenceSet(key T, set set.Bits64) {
	b.init()

	previousSet := (*b)[key]
	previousSet.Difference(set)
	(*b)[key] = previousSet
}

// For each key/bitset pair in [diff], removes each element of the bitset
// from the bitset associated with the key in [b].
// Keys in [diff] that are not in [b] are ignored.
// Bitset elements in [diff] that are not in the bitset associated with
// the key in [b] are ignored.
func (b *UniqueBag[T]) Difference(diff *UniqueBag[T]) {
	b.init()

	for key, previousSet := range *b {
		if previousSetDiff, exists := (*diff)[key]; exists {
			previousSet.Difference(previousSetDiff)
		}
		(*b)[key] = previousSet
	}
}

// Returns the bitset associated with [key].
func (b *UniqueBag[T]) GetSet(key T) set.Bits64 {
	return (*b)[key]
}

// Removes the bitset associated with [key].
func (b *UniqueBag[T]) RemoveSet(key T) {
	delete(*b, key)
}

// Returns the keys.
func (b *UniqueBag[T]) List() []T {
	return maps.Keys(*b)
}

// Returns a bag with the given [threshold] where each key is
// in the bag once for each element in the key's bitset.
func (b *UniqueBag[T]) Bag(threshold int) Bag[T] {
	bag := Bag[T]{
		counts: make(map[T]int, len(*b)),
	}
	bag.SetThreshold(threshold)
	for key, bs := range *b {
		bag.AddCount(key, bs.Len())
	}
	return bag
}

func (b *UniqueBag[T]) PrefixedString(prefix string) string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("UniqueBag[%T]: (Size = %d)", utils.Zero[T](), len(*b)))
	for key, set := range *b {
		sb.WriteString(fmt.Sprintf("\n%s    %v: %s", prefix, key, set))
	}

	return sb.String()
}

func (b *UniqueBag[_]) String() string {
	return b.PrefixedString("")
}

// Removes all key --> bitset pairs.
func (b *UniqueBag[_]) Clear() {
	clear(*b)
}
