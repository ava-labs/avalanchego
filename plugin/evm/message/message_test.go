// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/stretchr/testify/assert"
)

func TestEthTxs(t *testing.T) {
	assert := assert.New(t)

	msg := []byte("blah")
	builtMsg := Txs{
		Txs: msg,
	}
	builtMsgBytes, err := Build(&builtMsg)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, builtMsg.Bytes())

	parsedMsgIntf, err := Parse(builtMsgBytes)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, parsedMsgIntf.Bytes())

	parsedMsg, ok := parsedMsgIntf.(*Txs)
	assert.True(ok)

	assert.Equal(msg, parsedMsg.Txs)
}

func TestEthTxsTooLarge(t *testing.T) {
	assert := assert.New(t)

	builtMsg := Txs{
		Txs: utils.RandomBytes(1024 * units.KiB),
	}
	_, err := Build(&builtMsg)
	assert.Error(err)
}

func TestParseGibberish(t *testing.T) {
	assert := assert.New(t)

	randomBytes := utils.RandomBytes(256 * units.KiB)
	_, err := Parse(randomBytes)
	assert.Error(err)
}
