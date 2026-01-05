// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
)

// TODO: Benchmark what degree to use.
const treeDegree = 2

// Tree implements a set of numbers by tracking intervals. It supports adding
// and removing new values. It also allows checking if a value is included in
// the set.
//
// Tree is more space efficient than a map implementation if the values that it
// contains are continuous. The tree takes O(n) space where n is the number of
// continuous ranges that have been inserted into the tree.
//
// Add, Remove, and Contains all run in O(log n) where n is the number of
// continuous ranges that have been inserted into the tree.
type Tree struct {
	knownHeights *btree.BTreeG[*Interval]
	// If knownHeights contains the full range [0, MaxUint64], then
	// numKnownHeights overflows to 0.
	numKnownHeights uint64
}

// NewTree creates a new interval tree from the provided database.
//
// It is assumed that persisted intervals are non-overlapping. Providing a
// database with overlapping intervals will result in undefined behavior of the
// structure.
func NewTree(db database.Iteratee) (*Tree, error) {
	intervals, err := GetIntervals(db)
	if err != nil {
		return nil, err
	}

	var (
		knownHeights    = btree.NewG(treeDegree, (*Interval).Less)
		numKnownHeights uint64
	)
	for _, i := range intervals {
		knownHeights.ReplaceOrInsert(i)
		numKnownHeights += i.UpperBound - i.LowerBound + 1
	}
	return &Tree{
		knownHeights:    knownHeights,
		numKnownHeights: numKnownHeights,
	}, nil
}

func (t *Tree) Add(db database.KeyValueWriterDeleter, height uint64) error {
	var (
		newInterval = &Interval{
			LowerBound: height,
			UpperBound: height,
		}
		upper *Interval
		lower *Interval
	)
	t.knownHeights.AscendGreaterOrEqual(newInterval, func(item *Interval) bool {
		upper = item
		return false
	})
	if upper.Contains(height) {
		// height is already in the tree
		return nil
	}

	t.knownHeights.DescendLessOrEqual(newInterval, func(item *Interval) bool {
		lower = item
		return false
	})

	t.numKnownHeights++

	var (
		adjacentToLowerBound = upper.AdjacentToLowerBound(height)
		adjacentToUpperBound = lower.AdjacentToUpperBound(height)
	)
	switch {
	case adjacentToLowerBound && adjacentToUpperBound:
		// the upper and lower ranges should be merged
		if err := DeleteInterval(db, lower.UpperBound); err != nil {
			return err
		}
		upper.LowerBound = lower.LowerBound
		t.knownHeights.Delete(lower)
		return PutInterval(db, upper.UpperBound, lower.LowerBound)
	case adjacentToLowerBound:
		// the upper range should be extended by one on the lower side
		upper.LowerBound = height
		return PutInterval(db, upper.UpperBound, height)
	case adjacentToUpperBound:
		// the lower range should be extended by one on the upper side
		if err := DeleteInterval(db, lower.UpperBound); err != nil {
			return err
		}
		lower.UpperBound = height
		return PutInterval(db, height, lower.LowerBound)
	default:
		t.knownHeights.ReplaceOrInsert(newInterval)
		return PutInterval(db, height, height)
	}
}

func (t *Tree) Remove(db database.KeyValueWriterDeleter, height uint64) error {
	var (
		newInterval = &Interval{
			LowerBound: height,
			UpperBound: height,
		}
		higher *Interval
	)
	t.knownHeights.AscendGreaterOrEqual(newInterval, func(item *Interval) bool {
		higher = item
		return false
	})
	if !higher.Contains(height) {
		// height isn't in the tree
		return nil
	}

	t.numKnownHeights--

	switch {
	case higher.LowerBound == higher.UpperBound:
		t.knownHeights.Delete(higher)
		return DeleteInterval(db, higher.UpperBound)
	case higher.LowerBound == height:
		higher.LowerBound++
		return PutInterval(db, higher.UpperBound, higher.LowerBound)
	case higher.UpperBound == height:
		if err := DeleteInterval(db, higher.UpperBound); err != nil {
			return err
		}
		higher.UpperBound--
		return PutInterval(db, higher.UpperBound, higher.LowerBound)
	default:
		newInterval.LowerBound = higher.LowerBound
		newInterval.UpperBound = height - 1
		t.knownHeights.ReplaceOrInsert(newInterval)
		if err := PutInterval(db, newInterval.UpperBound, newInterval.LowerBound); err != nil {
			return err
		}

		higher.LowerBound = height + 1
		return PutInterval(db, higher.UpperBound, higher.LowerBound)
	}
}

func (t *Tree) Contains(height uint64) bool {
	var (
		i = &Interval{
			LowerBound: height,
			UpperBound: height,
		}
		higher *Interval
	)
	t.knownHeights.AscendGreaterOrEqual(i, func(item *Interval) bool {
		higher = item
		return false
	})
	return higher.Contains(height)
}

func (t *Tree) Flatten() []*Interval {
	intervals := make([]*Interval, 0, t.knownHeights.Len())
	t.knownHeights.Ascend(func(item *Interval) bool {
		intervals = append(intervals, item)
		return true
	})
	return intervals
}

// Len returns the number of heights in the tree; not the number of intervals.
//
// Because Len returns a uint64 and is describing the number of values in the
// range of uint64s, it will return 0 if the tree contains the full interval
// [0, MaxUint64].
func (t *Tree) Len() uint64 {
	return t.numKnownHeights
}
