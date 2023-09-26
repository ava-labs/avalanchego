// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ancestor

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

var (
	id1 = ids.GenerateTestID()
	id2 = ids.GenerateTestID()
	id3 = ids.GenerateTestID()
	id4 = ids.GenerateTestID()
	id5 = ids.GenerateTestID()
)

func TestAdd(t *testing.T) {
	tests := map[string]struct {
		method func(require *require.Assertions, at Tree)
	}{
		"should return self if not found": {
			method: func(require *require.Assertions, at Tree) {
				id := at.GetAncestor(id1)
				require.Equal(id1, id)
			},
		},
		"should add to tree and return id2 root": {
			method: func(require *require.Assertions, at Tree) {
				require.Zero(at.Len())
				at.Add(id1, id2)
				require.Equal(1, at.Len())
				require.True(at.Has(id1))
				result := at.GetAncestor(id1)
				require.Equal(id2, result)
			},
		},
		"should return ancestor id3 through id2": {
			method: func(require *require.Assertions, at Tree) {
				require.Zero(at.Len())
				at.Add(id1, id2)
				require.Equal(1, at.Len())
				at.Add(id2, id3)
				require.Equal(2, at.Len())
				require.True(at.Has(id2))
				result := at.GetAncestor(id1)
				require.Equal(id3, result)
			},
		},
		"should also return root id3 for another child": {
			method: func(require *require.Assertions, at Tree) {
				require.Zero(at.Len())
				at.Add(id1, id2)
				require.Equal(1, at.Len())
				at.Add(id2, id3)
				require.Equal(2, at.Len())
				at.Add(id4, id2)
				require.Equal(3, at.Len())
				result := at.GetAncestor(id4)
				require.Equal(id3, result)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			at := NewTree()
			test.method(require, at)
		})
	}
}

func TestRemove(t *testing.T) {
	tests := map[string]struct {
		method func(require *require.Assertions, at Tree)
	}{
		"removing root should not affect child roots": {
			method: func(require *require.Assertions, at Tree) {
				require.Zero(at.Len())
				at.Add(id1, id2)
				require.Equal(1, at.Len())
				at.Add(id2, id3)
				require.Equal(2, at.Len())
				at.Remove(id3)
				require.Equal(2, at.Len())
				require.True(at.Has(id1))
				require.True(at.Has(id2))
				require.False(at.Has(id3))
				id := at.GetAncestor(id2)
				require.Equal(id3, id)
				id = at.GetAncestor(id1)
				require.Equal(id3, id)
			},
		},
		"removing parent should change root": {
			method: func(require *require.Assertions, at Tree) {
				require.Zero(at.Len())
				at.Add(id1, id2)
				require.Equal(1, at.Len())
				at.Add(id2, id3)
				require.Equal(2, at.Len())
				at.Add(id3, id4)
				require.Equal(3, at.Len())
				id := at.GetAncestor(id1)
				require.Equal(id4, id)
				at.Remove(id3)
				require.Equal(2, at.Len())
				id = at.GetAncestor(id1)
				require.Equal(id3, id)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			at := NewTree()
			test.method(require, at)
		})
	}
}

func TestRemoveDescendants(t *testing.T) {
	tests := map[string]struct {
		method func(require *require.Assertions, at Tree)
	}{
		"remove root's descendants": {
			method: func(require *require.Assertions, at Tree) {
				require.Zero(at.Len())
				at.Add(id1, id2)
				require.Equal(1, at.Len())
				at.Add(id2, id3)
				require.Equal(2, at.Len())
				at.RemoveDescendants(id3)
				require.Zero(at.Len())
				require.False(at.Has(id1))
				require.False(at.Has(id2))
				require.False(at.Has(id3))
				id := at.GetAncestor(id2)
				require.Equal(id2, id)
				id = at.GetAncestor(id1)
				require.Equal(id1, id)
			},
		},
		"remove descendants": {
			method: func(require *require.Assertions, at Tree) {
				require.Zero(at.Len())
				at.Add(id1, id2)
				require.Equal(1, at.Len())
				at.Add(id2, id3)
				require.Equal(2, at.Len())
				at.Add(id3, id4)
				require.Equal(3, at.Len())
				at.Add(id4, id5)
				require.Equal(4, at.Len())
				at.RemoveDescendants(id3)
				require.Equal(1, at.Len())
				require.False(at.Has(id1))
				require.False(at.Has(id2))
				require.False(at.Has(id3))
				id := at.GetAncestor(id1)
				require.Equal(id, id1)
				id = at.GetAncestor(id3)
				require.Equal(id, id3)
				require.True(at.Has(id4))
				id = at.GetAncestor(id4)
				require.Equal(id5, id)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			at := NewTree()
			test.method(require, at)
		})
	}
}
