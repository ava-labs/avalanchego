// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package option

import (
	"github.com/stretchr/testify/assert"
)

func equal(assert *assert.Assertions, want, have Option) {
	assert.Equal(want.ID(), have.ID())
	assert.Equal(want.ParentID(), have.ParentID())
	assert.Equal(want.Block(), have.Block())
	assert.Equal(want.Bytes(), have.Bytes())
}
