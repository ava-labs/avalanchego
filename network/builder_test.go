// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	TestBuilder Builder
)

func TestBuildGetVersion(t *testing.T) {
	msg, err := TestBuilder.GetVersion()
	assert.NoError(t, err)
	assert.NotNil(t, msg)
	assert.Equal(t, GetVersion, msg.Op())

	parsedMsg, err := TestBuilder.Parse(msg.Bytes())
	assert.NoError(t, err)
	assert.NotNil(t, parsedMsg)
	assert.Equal(t, GetVersion, parsedMsg.Op())
}
