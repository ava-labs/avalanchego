// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package option

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestParse(t *testing.T) {
	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}

	assert := assert.New(t)

	builtOption, err := Build(parentID, innerBlockBytes)
	assert.NoError(err)

	builtOptionBytes := builtOption.Bytes()

	parsedOption, err := Parse(builtOptionBytes)
	assert.NoError(err)

	assert.Equal(builtOption.ID(), parsedOption.ID())
	assert.Equal(builtOption.ParentID(), parsedOption.ParentID())
	assert.Equal(builtOption.CoreBlock(), parsedOption.CoreBlock())
	assert.Equal(builtOption.Bytes(), parsedOption.Bytes())
}
