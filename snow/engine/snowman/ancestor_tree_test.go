// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestAdd(t *testing.T) {
	tests := map[string]struct {
		method func(require *require.Assertions, at AncestorTree)
	}{
		"should return false if not found": {
			method: func(require *require.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id := at.GetRoot(id1)
				require.Equal(id1, id)
			},
		},
		"should add to tree and return id2 root": {
			method: func(require *require.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				at.Add(id1, id2)
				require.True(at.Has(id1))
				result := at.GetRoot(id1)
				require.Equal(result, id2)
			},
		},
		"should return ancestor id3 through id2": {
			method: func(require *require.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				require.True(at.Has(id2))
				result := at.GetRoot(id1)
				require.Equal(result, id3)
			},
		},
		"should also return root id3 for another child": {
			method: func(require *require.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				id4 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				at.Add(id4, id2)
				result := at.GetRoot(id4)
				require.Equal(result, id3)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			at := NewAncestorTree()
			test.method(require, at)
		})
	}
}

func TestRemove(t *testing.T) {
	tests := map[string]struct {
		method func(require *require.Assertions, at AncestorTree)
	}{
		"removing root should not affect child roots": {
			method: func(require *require.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				at.Remove(id3)
				require.True(at.Has(id1))
				require.True(at.Has(id2))
				require.False(at.Has(id3))
				id := at.GetRoot(id2)
				require.Equal(id3, id)
				id = at.GetRoot(id1)
				require.Equal(id3, id)
			},
		},
		"removing parent should change root": {
			method: func(require *require.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				id4 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				at.Add(id3, id4)
				id := at.GetRoot(id1)
				require.Equal(id4, id)
				at.Remove(id3)
				id = at.GetRoot(id1)
				require.Equal(id3, id)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			at := NewAncestorTree()
			test.method(require, at)
		})
	}
}

func TestRemoveSubtree(t *testing.T) {
	tests := map[string]struct {
		method func(require *require.Assertions, at AncestorTree)
	}{
		"remove root's subtree": {
			method: func(require *require.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				at.RemoveSubtree(id3)
				require.False(at.Has(id1))
				require.False(at.Has(id2))
				require.False(at.Has(id3))
				id := at.GetRoot(id2)
				require.Equal(id2, id)
				id = at.GetRoot(id1)
				require.Equal(id1, id)
			},
		},
		"remove subtree": {
			method: func(require *require.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				id4 := ids.GenerateTestID()
				id5 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				at.Add(id3, id4)
				at.Add(id4, id5)
				at.RemoveSubtree(id3)
				require.False(at.Has(id1))
				require.False(at.Has(id2))
				require.False(at.Has(id3))
				id := at.GetRoot(id1)
				require.Equal(id, id1)
				id = at.GetRoot(id3)
				require.Equal(id, id3)
				require.True(at.Has(id4))
				id = at.GetRoot(id4)
				require.Equal(id5, id)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			at := NewAncestorTree()
			test.method(require, at)
		})
	}
}
