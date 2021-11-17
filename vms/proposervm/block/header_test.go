// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/stretchr/testify/assert"
)

func equalHeader(assert *assert.Assertions, want, have Header) {
	assert.Equal(want.ChainID(), have.ChainID())
	assert.Equal(want.ParentID(), have.ParentID())
	assert.Equal(want.BodyID(), have.BodyID())
}
