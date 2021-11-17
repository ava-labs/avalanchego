// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func TestAdd(t *testing.T) {
	tests := map[string]struct {
		method func(assert *assert.Assertions, at AncestorTree)
	}{
		"should return false if not found": {
			method: func(assert *assert.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id := at.GetRoot(id1)
				assert.Equal(id1, id)
			},
		},
		"should add to tree and return id2 root": {
			method: func(assert *assert.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				at.Add(id1, id2)
				assert.True(at.Has(id1))
				result := at.GetRoot(id1)
				assert.Equal(result, id2)
			},
		},
		"should return ancestor id3 through id2": {
			method: func(assert *assert.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				assert.True(at.Has(id2))
				result := at.GetRoot(id1)
				assert.Equal(result, id3)
			},
		},
		"should also return root id3 for another child": {
			method: func(assert *assert.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				id4 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				at.Add(id4, id2)
				result := at.GetRoot(id4)
				assert.Equal(result, id3)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			at := NewAncestorTree()
			test.method(assert, at)
		})
	}
}

func TestRemove(t *testing.T) {
	tests := map[string]struct {
		method func(assert *assert.Assertions, at AncestorTree)
	}{
		"removing root should not affect child roots": {
			method: func(assert *assert.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				at.Remove(id3)
				assert.True(at.Has(id1))
				assert.True(at.Has(id2))
				assert.False(at.Has(id3))
				id := at.GetRoot(id2)
				assert.Equal(id3, id)
				id = at.GetRoot(id1)
				assert.Equal(id3, id)
			},
		},
		"removing parent should change root": {
			method: func(assert *assert.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				id4 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				at.Add(id3, id4)
				id := at.GetRoot(id1)
				assert.Equal(id4, id)
				at.Remove(id3)
				id = at.GetRoot(id1)
				assert.Equal(id3, id)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			at := NewAncestorTree()
			test.method(assert, at)
		})
	}
}

func TestRemoveSubtree(t *testing.T) {
	tests := map[string]struct {
		method func(assert *assert.Assertions, at AncestorTree)
	}{
		"remove root's subtree": {
			method: func(assert *assert.Assertions, at AncestorTree) {
				id1 := ids.GenerateTestID()
				id2 := ids.GenerateTestID()
				id3 := ids.GenerateTestID()
				at.Add(id1, id2)
				at.Add(id2, id3)
				at.RemoveSubtree(id3)
				assert.False(at.Has(id1))
				assert.False(at.Has(id2))
				assert.False(at.Has(id3))
				id := at.GetRoot(id2)
				assert.Equal(id2, id)
				id = at.GetRoot(id1)
				assert.Equal(id1, id)
			},
		},
		"remove subtree": {
			method: func(assert *assert.Assertions, at AncestorTree) {
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
				assert.False(at.Has(id1))
				assert.False(at.Has(id2))
				assert.False(at.Has(id3))
				id := at.GetRoot(id1)
				assert.Equal(id, id1)
				id = at.GetRoot(id3)
				assert.Equal(id, id3)
				assert.True(at.Has(id4))
				id = at.GetRoot(id4)
				assert.Equal(id5, id)
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			at := NewAncestorTree()
			test.method(assert, at)
		})
	}
}
