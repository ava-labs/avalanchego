// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interval

import (
	"math"
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
			require.NoError(tree.Add(db, i))
		}
	}
	return tree
}

func TestTreeAdd(t *testing.T) {
	tests := []struct {
		name        string
		toAdd       []*Interval
		expected    []*Interval
		expectedLen uint64
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
			expectedLen: 1,
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
			expectedLen: 2,
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
			expectedLen: 2,
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
			expectedLen: 3,
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
			expectedLen: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			treeFromAdditions := newTree(require, db, test.toAdd)
			require.Equal(test.expected, treeFromAdditions.Flatten())
			require.Equal(test.expectedLen, treeFromAdditions.Len())

			treeFromDB := newTree(require, db, nil)
			require.Equal(test.expected, treeFromDB.Flatten())
			require.Equal(test.expectedLen, treeFromDB.Len())
		})
	}
}

func TestTreeRemove(t *testing.T) {
	tests := []struct {
		name        string
		toAdd       []*Interval
		toRemove    []*Interval
		expected    []*Interval
		expectedLen uint64
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
			expected:    []*Interval{},
			expectedLen: 0,
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
			expectedLen: 1,
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
			expectedLen: 1,
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
			expectedLen: 2,
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
			expectedLen: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			treeFromModifications := newTree(require, db, test.toAdd)
			for _, toRemove := range test.toRemove {
				for i := toRemove.LowerBound; i <= toRemove.UpperBound; i++ {
					require.NoError(treeFromModifications.Remove(db, i))
				}
			}
			require.Equal(test.expected, treeFromModifications.Flatten())
			require.Equal(test.expectedLen, treeFromModifications.Len())

			treeFromDB := newTree(require, db, nil)
			require.Equal(test.expected, treeFromDB.Flatten())
			require.Equal(test.expectedLen, treeFromDB.Len())
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

func TestTreeLenOverflow(t *testing.T) {
	require := require.New(t)

	db := memdb.New()
	require.NoError(PutInterval(db, math.MaxUint64, 0))

	tree, err := NewTree(db)
	require.NoError(err)
	require.Zero(tree.Len())
	require.True(tree.Contains(0))
	require.True(tree.Contains(math.MaxUint64 / 2))
	require.True(tree.Contains(math.MaxUint64))

	require.NoError(tree.Remove(db, 5))
	require.Equal(uint64(math.MaxUint64), tree.Len())

	require.NoError(tree.Add(db, 5))
	require.Zero(tree.Len())
}
