// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMultiIterator(t *testing.T) {
	assert := assert.New(t)

	stakers0 := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

	stakers1 := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(3, 0),
		},
	}

	stakers := []*Staker{
		stakers0[0],
		stakers1[0],
		stakers0[1],
		stakers1[1],
	}

	it := NewMultiIterator(
		EmptyIterator,
		NewSliceIterator(stakers0),
		EmptyIterator,
		NewSliceIterator(stakers1),
		EmptyIterator,
	)
	for _, staker := range stakers {
		assert.True(it.Next())
		assert.Equal(staker, it.Value())
	}
	assert.False(it.Next())
	it.Release()
	assert.False(it.Next())
}

func TestMultiIteratorEarlyRelease(t *testing.T) {
	assert := assert.New(t)

	stakers0 := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
	}

	stakers1 := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(3, 0),
		},
	}

	it := NewMultiIterator(
		EmptyIterator,
		NewSliceIterator(stakers0),
		EmptyIterator,
		NewSliceIterator(stakers1),
		EmptyIterator,
	)
	assert.True(it.Next())
	it.Release()
	assert.False(it.Next())
}
