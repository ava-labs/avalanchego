// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"github.com/google/btree"

	"github.com/ava-labs/avalanchego/database"
)

const treeDegree = 2

type Tree struct {
	db          database.Database
	knownBlocks *btree.BTreeG[*interval]
}

func NewTree(db database.Database) (*Tree, error) {
	intervals, err := GetIntervals(db)
	if err != nil {
		return nil, err
	}

	knownBlocks := btree.NewG(treeDegree, (*interval).Less)
	for _, i := range intervals {
		knownBlocks.ReplaceOrInsert(i)
	}
	return &Tree{
		db:          db,
		knownBlocks: knownBlocks,
	}, nil
}

func (t *Tree) Add(height uint64) error {
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
		return nil
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
		if err := DeleteInterval(t.db, lower.upperBound); err != nil {
			return err
		}
		upper.lowerBound = lower.lowerBound
		t.knownBlocks.Delete(lower)
		return PutInterval(t.db, upper.upperBound, lower.lowerBound)
	case adjacentToLowerBound:
		// the upper range should be extended by one on the lower side
		upper.lowerBound = height
		return PutInterval(t.db, upper.upperBound, height)
	case adjacentToUpperBound:
		// the lower range should be extended by one on the upper side
		if err := DeleteInterval(t.db, lower.upperBound); err != nil {
			return err
		}
		lower.upperBound = height
		return PutInterval(t.db, height, lower.lowerBound)
	default:
		t.knownBlocks.ReplaceOrInsert(newInterval)
		return PutInterval(t.db, height, height)
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
