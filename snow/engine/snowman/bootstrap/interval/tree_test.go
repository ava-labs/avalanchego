// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"testing"

	"github.com/google/btree"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func flatten(tree *btree.BTreeG[*interval]) []*interval {
	intervals := make([]*interval, 0, tree.Len())
	tree.Ascend(func(item *interval) bool {
		intervals = append(intervals, item)
		return true
	})
	return intervals
}

func newTree(require *require.Assertions, db database.Database, intervals []*interval) *Tree {
	tree, err := NewTree(db)
	require.NoError(err)

	for _, toAdd := range intervals {
		for i := toAdd.lowerBound; i <= toAdd.upperBound; i++ {
			require.NoError(tree.Add(i))
		}
	}
	return tree
}

func TestTreeAdd(t *testing.T) {
	tests := []struct {
		name     string
		toAdd    []*interval
		expected []*interval
	}{
		{
			name: "single addition",
			toAdd: []*interval{
				{
					lowerBound: 10,
					upperBound: 10,
				},
			},
			expected: []*interval{
				{
					lowerBound: 10,
					upperBound: 10,
				},
			},
		},
		{
			name: "extend above",
			toAdd: []*interval{
				{
					lowerBound: 10,
					upperBound: 11,
				},
			},
			expected: []*interval{
				{
					lowerBound: 10,
					upperBound: 11,
				},
			},
		},
		{
			name: "extend below",
			toAdd: []*interval{
				{
					lowerBound: 11,
					upperBound: 11,
				},
				{
					lowerBound: 10,
					upperBound: 10,
				},
			},
			expected: []*interval{
				{
					lowerBound: 10,
					upperBound: 11,
				},
			},
		},
		{
			name: "merge",
			toAdd: []*interval{
				{
					lowerBound: 10,
					upperBound: 10,
				},
				{
					lowerBound: 12,
					upperBound: 12,
				},
				{
					lowerBound: 11,
					upperBound: 11,
				},
			},
			expected: []*interval{
				{
					lowerBound: 10,
					upperBound: 12,
				},
			},
		},
		{
			name: "ignore duplicate",
			toAdd: []*interval{
				{
					lowerBound: 10,
					upperBound: 11,
				},
				{
					lowerBound: 11,
					upperBound: 11,
				},
			},
			expected: []*interval{
				{
					lowerBound: 10,
					upperBound: 11,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			treeFromAdditions := newTree(require, db, test.toAdd)
			require.Equal(test.expected, flatten(treeFromAdditions.knownHeights))

			treeFromDB := newTree(require, db, nil)
			require.Equal(test.expected, flatten(treeFromDB.knownHeights))
		})
	}
}

func TestTreeContains(t *testing.T) {
	tests := []struct {
		name     string
		tree     []*interval
		height   uint64
		expected bool
	}{
		{
			name: "below",
			tree: []*interval{
				{
					lowerBound: 10,
					upperBound: 10,
				},
			},
			height:   9,
			expected: false,
		},
		{
			name: "above",
			tree: []*interval{
				{
					lowerBound: 10,
					upperBound: 10,
				},
			},
			height:   11,
			expected: false,
		},
		{
			name: "equal both",
			tree: []*interval{
				{
					lowerBound: 10,
					upperBound: 10,
				},
			},
			height:   10,
			expected: true,
		},
		{
			name: "equal lower",
			tree: []*interval{
				{
					lowerBound: 10,
					upperBound: 11,
				},
			},
			height:   10,
			expected: true,
		},
		{
			name: "equal upper",
			tree: []*interval{
				{
					lowerBound: 9,
					upperBound: 10,
				},
			},
			height:   10,
			expected: true,
		},
		{
			name: "inside",
			tree: []*interval{
				{
					lowerBound: 9,
					upperBound: 11,
				},
			},
			height:   10,
			expected: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			tree := newTree(require, memdb.New(), test.tree)
			require.Equal(test.expected, tree.Contains(test.height))
		})
	}
}
