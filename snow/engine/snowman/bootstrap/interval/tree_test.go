// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
)

func newTree(require *require.Assertions, db database.Database, intervals []*Interval) *Tree {
	tree, err := NewTree(db)
	require.NoError(err)

	for _, toAdd := range intervals {
		for i := toAdd.LowerBound; i <= toAdd.UpperBound; i++ {
			require.NoError(tree.Add(i))
		}
	}
	return tree
}

func TestTreeAdd(t *testing.T) {
	tests := []struct {
		name     string
		toAdd    []*Interval
		expected []*Interval
	}{
		{
			name: "single addition",
			toAdd: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
			expected: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
		},
		{
			name: "extend above",
			toAdd: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 11,
				},
			},
			expected: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 11,
				},
			},
		},
		{
			name: "extend below",
			toAdd: []*Interval{
				{
					LowerBound: 11,
					UpperBound: 11,
				},
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
			expected: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 11,
				},
			},
		},
		{
			name: "merge",
			toAdd: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
				{
					LowerBound: 12,
					UpperBound: 12,
				},
				{
					LowerBound: 11,
					UpperBound: 11,
				},
			},
			expected: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 12,
				},
			},
		},
		{
			name: "ignore duplicate",
			toAdd: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 11,
				},
				{
					LowerBound: 11,
					UpperBound: 11,
				},
			},
			expected: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 11,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			treeFromAdditions := newTree(require, db, test.toAdd)
			require.Equal(test.expected, treeFromAdditions.Flatten())

			treeFromDB := newTree(require, db, nil)
			require.Equal(test.expected, treeFromDB.Flatten())
		})
	}
}

func TestTreeRemove(t *testing.T) {
	tests := []struct {
		name     string
		toAdd    []*Interval
		toRemove []*Interval
		expected []*Interval
	}{
		{
			name: "single removal",
			toAdd: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
			toRemove: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
			expected: []*Interval{},
		},
		{
			name: "reduce above",
			toAdd: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 11,
				},
			},
			toRemove: []*Interval{
				{
					LowerBound: 11,
					UpperBound: 11,
				},
			},
			expected: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
		},
		{
			name: "reduce below",
			toAdd: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 11,
				},
			},
			toRemove: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
			expected: []*Interval{
				{
					LowerBound: 11,
					UpperBound: 11,
				},
			},
		},
		{
			name: "split",
			toAdd: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 12,
				},
			},
			toRemove: []*Interval{
				{
					LowerBound: 11,
					UpperBound: 11,
				},
			},
			expected: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
				{
					LowerBound: 12,
					UpperBound: 12,
				},
			},
		},
		{
			name: "ignore missing",
			toAdd: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
			toRemove: []*Interval{
				{
					LowerBound: 11,
					UpperBound: 11,
				},
			},
			expected: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			treeFromModifications := newTree(require, db, test.toAdd)
			for _, toRemove := range test.toRemove {
				for i := toRemove.LowerBound; i <= toRemove.UpperBound; i++ {
					require.NoError(treeFromModifications.Remove(i))
				}
			}
			require.Equal(test.expected, treeFromModifications.Flatten())

			treeFromDB := newTree(require, db, nil)
			require.Equal(test.expected, treeFromDB.Flatten())
		})
	}
}

func TestTreeContains(t *testing.T) {
	tests := []struct {
		name     string
		tree     []*Interval
		height   uint64
		expected bool
	}{
		{
			name: "below",
			tree: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
			height:   9,
			expected: false,
		},
		{
			name: "above",
			tree: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
			height:   11,
			expected: false,
		},
		{
			name: "equal both",
			tree: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 10,
				},
			},
			height:   10,
			expected: true,
		},
		{
			name: "equal lower",
			tree: []*Interval{
				{
					LowerBound: 10,
					UpperBound: 11,
				},
			},
			height:   10,
			expected: true,
		},
		{
			name: "equal upper",
			tree: []*Interval{
				{
					LowerBound: 9,
					UpperBound: 10,
				},
			},
			height:   10,
			expected: true,
		},
		{
			name: "inside",
			tree: []*Interval{
				{
					LowerBound: 9,
					UpperBound: 11,
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
