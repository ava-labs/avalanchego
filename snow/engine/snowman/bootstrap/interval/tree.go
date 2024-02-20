// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"github.com/google/btree"
)

type Tree struct {
	knownBlocks *btree.BTreeG[*interval]
}

func NewTree() *Tree {
	knownBlocks := btree.NewG(2, (*interval).Less)
	return &Tree{
		knownBlocks: knownBlocks,
	}
}

func (t *Tree) Add(height uint64) {
	var (
		newInterval = &interval{
			lowerBound: height,
			upperBound: height,
		}
		upper *interval
		lower *interval
	)
	t.knownBlocks.AscendGreaterOrEqual(newInterval, func(item *interval) bool {
		upper = item
		return false
	})
	if upper.Contains(height) {
		// height is already in the tree
		return
	}

	t.knownBlocks.DescendLessOrEqual(newInterval, func(item *interval) bool {
		lower = item
		return false
	})

	var (
		adjacentToLowerBound = upper.AdjacentToLowerBound(height)
		adjacentToUpperBound = lower.AdjacentToUpperBound(height)
	)
	switch {
	case adjacentToLowerBound && adjacentToUpperBound:
		// the upper and lower ranges should be merged
		upper.lowerBound = lower.lowerBound
		t.knownBlocks.Delete(lower)
	case adjacentToLowerBound:
		// the upper range should be extended by one on the lower side
		upper.lowerBound = height
	case adjacentToUpperBound:
		// the lower range should be extended by one on the upper side
		lower.upperBound = height
	default:
		t.knownBlocks.ReplaceOrInsert(newInterval)
	}
}

func (t *Tree) Contains(height uint64) bool {
	var (
		i = &interval{
			lowerBound: height,
			upperBound: height,
		}
		higher *interval
	)
	t.knownBlocks.AscendGreaterOrEqual(i, func(item *interval) bool {
		higher = item
		return false
	})
	return higher.Contains(height)
}
