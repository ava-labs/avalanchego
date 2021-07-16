// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package option

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func TestParse(t *testing.T) {
	assert := assert.New(t)

	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}

	builtOption, err := Build(parentID, innerBlockBytes)
	assert.NoError(err)

	builtOptionBytes := builtOption.Bytes()

	parsedOption, err := Parse(builtOptionBytes)
	assert.NoError(err)

	equal(assert, builtOption, parsedOption)
}
