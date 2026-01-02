// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ancestor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	id1 = ids.GenerateTestID()
	id2 = ids.GenerateTestID()
	id3 = ids.GenerateTestID()
	id4 = ids.GenerateTestID()
)

func TestAdd(t *testing.T) {
	tests := map[string]struct {
		initial  Tree
		blkID    ids.ID
		parentID ids.ID
		expected Tree
	}{
		"add to empty tree": {
			initial: &tree{
				childToParent:    make(map[ids.ID]ids.ID),
				parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
			},
			blkID:    id1,
			parentID: id2,
			expected: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
				},
			},
		},
		"add new parent": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
				},
			},
			blkID:    id3,
			parentID: id4,
			expected: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
					id3: id4,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
					id4: set.Of(id3),
				},
			},
		},
		"add new block to existing parent": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
				},
			},
			blkID:    id3,
			parentID: id2,
			expected: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
					id3: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1, id3),
				},
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			at := test.initial
			at.Add(test.blkID, test.parentID)
			require.Equal(t, test.expected, at)
		})
	}
}

func TestRemove(t *testing.T) {
	tests := map[string]struct {
		initial  Tree
		blkID    ids.ID
		expected Tree
	}{
		"remove from empty tree": {
			initial: &tree{
				childToParent:    make(map[ids.ID]ids.ID),
				parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
			},
			blkID: id1,
			expected: &tree{
				childToParent:    make(map[ids.ID]ids.ID),
				parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
			},
		},
		"remove block and parent from tree": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
				},
			},
			blkID: id1,
			expected: &tree{
				childToParent:    make(map[ids.ID]ids.ID),
				parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
			},
		},
		"remove block and not parent from tree": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
					id3: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1, id3),
				},
			},
			blkID: id1,
			expected: &tree{
				childToParent: map[ids.ID]ids.ID{
					id3: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id3),
				},
			},
		},
		"remove untracked block from tree": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
				},
			},
			blkID: id2,
			expected: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
				},
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			at := test.initial
			at.Remove(test.blkID)
			require.Equal(t, test.expected, at)
		})
	}
}

func TestRemoveDescendants(t *testing.T) {
	tests := map[string]struct {
		initial  Tree
		blkID    ids.ID
		expected Tree
	}{
		"remove from empty tree": {
			initial: &tree{
				childToParent:    make(map[ids.ID]ids.ID),
				parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
			},
			blkID: id1,
			expected: &tree{
				childToParent:    make(map[ids.ID]ids.ID),
				parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
			},
		},
		"remove block and parent from tree": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
				},
			},
			blkID: id1,
			expected: &tree{
				childToParent:    make(map[ids.ID]ids.ID),
				parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
			},
		},
		"remove block and not parent from tree": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
					id3: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1, id3),
				},
			},
			blkID: id1,
			expected: &tree{
				childToParent: map[ids.ID]ids.ID{
					id3: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id3),
				},
			},
		},
		"remove untracked block from tree": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
				},
			},
			blkID: id3,
			expected: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
				},
			},
		},
		"remove children from tree": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
					id3: id2,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1, id3),
				},
			},
			blkID: id2,
			expected: &tree{
				childToParent:    make(map[ids.ID]ids.ID),
				parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
			},
		},
		"remove grand child from tree": {
			initial: &tree{
				childToParent: map[ids.ID]ids.ID{
					id1: id2,
					id2: id3,
				},
				parentToChildren: map[ids.ID]set.Set[ids.ID]{
					id2: set.Of(id1),
					id3: set.Of(id2),
				},
			},
			blkID: id3,
			expected: &tree{
				childToParent:    make(map[ids.ID]ids.ID),
				parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			at := test.initial
			at.RemoveDescendants(test.blkID)
			require.Equal(t, test.expected, at)
		})
	}
}

func TestHas(t *testing.T) {
	require := require.New(t)

	at := NewTree()
	require.False(at.Has(id1))
	require.False(at.Has(id2))
	require.False(at.Has(id3))

	at.Add(id1, id2)
	require.True(at.Has(id1))
	require.False(at.Has(id2))
	require.False(at.Has(id3))

	at.Add(id2, id3)
	require.True(at.Has(id1))
	require.True(at.Has(id2))
	require.False(at.Has(id3))
}

func TestGetAncestor(t *testing.T) {
	require := require.New(t)

	at := NewTree()
	require.Equal(id1, at.GetAncestor(id1))
	require.Equal(id2, at.GetAncestor(id2))
	require.Equal(id3, at.GetAncestor(id3))
	require.Equal(id4, at.GetAncestor(id4))

	at.Add(id1, id2)
	require.Equal(id2, at.GetAncestor(id1))
	require.Equal(id2, at.GetAncestor(id2))
	require.Equal(id3, at.GetAncestor(id3))
	require.Equal(id4, at.GetAncestor(id4))

	at.Add(id2, id3)
	require.Equal(id3, at.GetAncestor(id1))
	require.Equal(id3, at.GetAncestor(id2))
	require.Equal(id3, at.GetAncestor(id3))
	require.Equal(id4, at.GetAncestor(id4))

	at.Add(id4, id3)
	require.Equal(id3, at.GetAncestor(id1))
	require.Equal(id3, at.GetAncestor(id2))
	require.Equal(id3, at.GetAncestor(id3))
	require.Equal(id3, at.GetAncestor(id4))
}

func TestLen(t *testing.T) {
	require := require.New(t)

	at := NewTree()
	require.Zero(at.Len())

	at.Add(id1, id2)
	require.Equal(1, at.Len())

	at.Add(id2, id3)
	require.Equal(2, at.Len())

	at.Add(id4, id3)
	require.Equal(3, at.Len())
}
