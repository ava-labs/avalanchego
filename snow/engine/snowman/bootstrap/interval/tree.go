// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
)

const treeDegree = 2

type Tree struct {
	db           database.Database
	knownHeights *btree.BTreeG[*Interval]
}

func NewTree(db database.Database) (*Tree, error) {
	intervals, err := GetIntervals(db)
	if err != nil {
		return nil, err
	}

	knownHeights := btree.NewG(treeDegree, (*Interval).Less)
	for _, i := range intervals {
		knownHeights.ReplaceOrInsert(i)
	}
	return &Tree{
		db:           db,
		knownHeights: knownHeights,
	}, nil
}

func (t *Tree) Add(height uint64) error {
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

	var (
		adjacentToLowerBound = upper.AdjacentToLowerBound(height)
		adjacentToUpperBound = lower.AdjacentToUpperBound(height)
	)
	switch {
	case adjacentToLowerBound && adjacentToUpperBound:
		// the upper and lower ranges should be merged
		if err := DeleteInterval(t.db, lower.UpperBound); err != nil {
			return err
		}
		upper.LowerBound = lower.LowerBound
		t.knownHeights.Delete(lower)
		return PutInterval(t.db, upper.UpperBound, lower.LowerBound)
	case adjacentToLowerBound:
		// the upper range should be extended by one on the lower side
		upper.LowerBound = height
		return PutInterval(t.db, upper.UpperBound, height)
	case adjacentToUpperBound:
		// the lower range should be extended by one on the upper side
		if err := DeleteInterval(t.db, lower.UpperBound); err != nil {
			return err
		}
		lower.UpperBound = height
		return PutInterval(t.db, height, lower.LowerBound)
	default:
		t.knownHeights.ReplaceOrInsert(newInterval)
		return PutInterval(t.db, height, height)
	}
}

func (t *Tree) Remove(height uint64) error {
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

	switch {
	case higher.LowerBound == higher.UpperBound:
		t.knownHeights.Delete(higher)
		return DeleteInterval(t.db, higher.UpperBound)
	case higher.LowerBound == height:
		higher.LowerBound++
		return PutInterval(t.db, higher.UpperBound, higher.LowerBound)
	case higher.UpperBound == height:
		if err := DeleteInterval(t.db, higher.UpperBound); err != nil {
			return err
		}
		higher.UpperBound--
		return PutInterval(t.db, higher.UpperBound, higher.LowerBound)
	default:
		newInterval.LowerBound = higher.LowerBound
		newInterval.UpperBound = height - 1
		t.knownHeights.ReplaceOrInsert(newInterval)
		if err := PutInterval(t.db, newInterval.UpperBound, newInterval.LowerBound); err != nil {
			return err
		}

		higher.LowerBound = height + 1
		return PutInterval(t.db, higher.UpperBound, higher.LowerBound)
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
