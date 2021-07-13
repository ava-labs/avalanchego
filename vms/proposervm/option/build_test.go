// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package option

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestBuild(t *testing.T) {
	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}

	assert := assert.New(t)

	builtOption, err := Build(parentID, innerBlockBytes)
	assert.NoError(err)

	assert.Equal(parentID, builtOption.ParentID())
	assert.Equal(innerBlockBytes, builtOption.Block())
}
