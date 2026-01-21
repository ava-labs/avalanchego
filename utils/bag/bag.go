// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bag

import (
	"fmt"
	"strings"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
)

const minBagSize = 16

// Bag is a multiset.
type Bag[T comparable] struct {
	counts map[T]int
	size   int

	threshold    int
	metThreshold set.Set[T]
}

// Of returns a Bag initialized with [elts]
func Of[T comparable](elts ...T) Bag[T] {
	var b Bag[T]
	b.Add(elts...)
	return b
}

func (b *Bag[T]) init() {
	if b.counts == nil {
		b.counts = make(map[T]int, minBagSize)
	}
}

// SetThreshold sets the number of times an element must be added to be contained in
// the threshold set.
func (b *Bag[_]) SetThreshold(threshold int) {
	if b.threshold == threshold {
		return
	}

	b.threshold = threshold
	b.metThreshold.Clear()
	for vote, count := range b.counts {
		if count >= threshold {
			b.metThreshold.Add(vote)
		}
	}
}

// Add increases the number of times each element has been seen by one.
func (b *Bag[T]) Add(elts ...T) {
	for _, elt := range elts {
		b.AddCount(elt, 1)
	}
}

// AddCount increases the number of times the element has been seen by [count].
// If [count] <= 0 this is a no-op.
func (b *Bag[T]) AddCount(elt T, count int) {
	if count <= 0 {
		return
	}

	b.init()

	totalCount := b.counts[elt] + count
	b.counts[elt] = totalCount
	b.size += count

	if totalCount >= b.threshold {
		b.metThreshold.Add(elt)
	}
}

// Count returns the number of [elt] in the bag.
func (b *Bag[T]) Count(elt T) int {
	return b.counts[elt]
}

// Len returns the number of elements in the bag.
func (b *Bag[_]) Len() int {
	return b.size
}

// List returns a list of unique elements that have been added.
// The returned list doesn't have duplicates.
func (b *Bag[T]) List() []T {
	return maps.Keys(b.counts)
}

// Equals returns true if the bags contain the same elements
func (b *Bag[T]) Equals(other Bag[T]) bool {
	return b.size == other.size && maps.Equal(b.counts, other.counts)
}

// Mode returns the most common element in the bag and the count of that element.
// If there's a tie, any of the tied element may be returned.
func (b *Bag[T]) Mode() (T, int) {
	var (
		mode     T
		modeFreq int
	)
	for elt, count := range b.counts {
		if count > modeFreq {
			mode = elt
			modeFreq = count
		}
	}

	return mode, modeFreq
}

// Threshold returns the elements that have been seen at least threshold times.
func (b *Bag[T]) Threshold() set.Set[T] {
	return b.metThreshold
}

// Returns a bag with the elements of this bag that return true for [filterFunc],
// along with their counts.
// For example, if X is in this bag with count 5, and filterFunc(X) returns true,
// then the returned bag contains X with count 5.
func (b *Bag[T]) Filter(filterFunc func(T) bool) Bag[T] {
	newBag := Bag[T]{}
	for vote, count := range b.counts {
		if filterFunc(vote) {
			newBag.AddCount(vote, count)
		}
	}
	return newBag
}

// Returns:
// 1. A bag containing the elements of this bag that return false for [splitFunc].
// 2. A bag containing the elements of this bag that return true for [splitFunc].
// Counts are preserved in the returned bags.
// For example, if X is in this bag with count 5, and splitFunc(X) is false,
// then the first returned bag has X in it with count 5.
func (b *Bag[T]) Split(splitFunc func(T) bool) [2]Bag[T] {
	splitVotes := [2]Bag[T]{}
	for vote, count := range b.counts {
		if splitFunc(vote) {
			splitVotes[1].AddCount(vote, count)
		} else {
			splitVotes[0].AddCount(vote, count)
		}
	}
	return splitVotes
}

// Remove all instances of [elt] from the bag.
func (b *Bag[T]) Remove(elt T) {
	count := b.counts[elt]
	delete(b.counts, elt)
	b.size -= count
}

func (b *Bag[T]) PrefixedString(prefix string) string {
	sb := strings.Builder{}

	sb.WriteString(fmt.Sprintf("Bag[%T]: (Size = %d)", utils.Zero[T](), b.Len()))
	for elt, count := range b.counts {
		sb.WriteString(fmt.Sprintf("\n%s    %v: %d", prefix, elt, count))
	}

	return sb.String()
}

func (b *Bag[_]) String() string {
	return b.PrefixedString("")
}

func (b *Bag[T]) Clone() Bag[T] {
	var clone Bag[T]
	for id, count := range b.counts {
		clone.AddCount(id, count)
	}
	return clone
}
