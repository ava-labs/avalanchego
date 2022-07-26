// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmptyIterator(t *testing.T) {
	assert := assert.New(t)
	assert.False(EmptyIterator.Next())

	EmptyIterator.Release()

	assert.False(EmptyIterator.Next())
	assert.Nil(EmptyIterator.Value())
}
