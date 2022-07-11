// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMaskedIterator(t *testing.T) {
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
	maskedStakers := map[ids.ID]*Staker{
		stakers[0].TxID: stakers[0],
		stakers[2].TxID: stakers[2],
	}

	it := NewMaskedIterator(
		NewSliceIterator(stakers...),
		maskedStakers,
	)
	for _, staker := range stakers[1:2] {
		assert.True(it.Next())
		assert.Equal(staker, it.Value())
	}
	assert.False(it.Next())
	it.Release()
	assert.False(it.Next())
}
