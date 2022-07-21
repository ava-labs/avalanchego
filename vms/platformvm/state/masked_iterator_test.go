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
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(3, 0),
		},
	}
	maskedStakers := map[ids.ID]*Staker{
		stakers[0].TxID: stakers[0],
		stakers[2].TxID: stakers[2],
		stakers[3].TxID: stakers[3],
	}

	it := NewMaskedIterator(
		NewSliceIterator(stakers[:3]...),
		maskedStakers,
	)

	assert.True(it.Next())
	assert.Equal(stakers[1], it.Value())

	assert.False(it.Next())
	it.Release()
	assert.False(it.Next())
}
