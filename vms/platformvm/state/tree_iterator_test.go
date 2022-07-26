// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/google/btree"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestTreeIterator(t *testing.T) {
	assert := assert.New(t)
	stakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

	tree := btree.New(defaultTreeDegree)
	for _, staker := range stakers {
		assert.Nil(tree.ReplaceOrInsert(staker))
	}

	it := NewTreeIterator(tree)
	for _, staker := range stakers {
		assert.True(it.Next())
		assert.Equal(staker, it.Value())
	}
	assert.False(it.Next())
	it.Release()
}

func TestTreeIteratorNil(t *testing.T) {
	assert := assert.New(t)
	it := NewTreeIterator(nil)
	assert.False(it.Next())
	it.Release()
}

func TestTreeIteratorEarlyRelease(t *testing.T) {
	assert := assert.New(t)
	stakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

	tree := btree.New(defaultTreeDegree)
	for _, staker := range stakers {
		assert.Nil(tree.ReplaceOrInsert(staker))
	}

	it := NewTreeIterator(tree)
	assert.True(it.Next())
	it.Release()
	assert.False(it.Next())
}
