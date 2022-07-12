// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSliceIterator(t *testing.T) {
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
	}

	it := NewSliceIterator(stakers...)
	for _, staker := range stakers {
		assert.True(it.Next())
		assert.Equal(staker, it.Value())
	}
	assert.False(it.Next())
	it.Release()
}
