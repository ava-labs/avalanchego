// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/stretchr/testify/assert"
)

func TestTxs(t *testing.T) {
	assert := assert.New(t)

	msg := []byte("blah")
	builtMsg := Txs{
		Txs: msg,
	}
	codec, err := BuildCodec()
	assert.NoError(err)
	builtMsgBytes, err := BuildMessage(codec, &builtMsg)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, builtMsg.Bytes())

	parsedMsgIntf, err := ParseMessage(codec, builtMsgBytes)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, parsedMsgIntf.Bytes())

	parsedMsg, ok := parsedMsgIntf.(*Txs)
	assert.True(ok)

	assert.Equal(msg, parsedMsg.Txs)
}

func TestTxsTooLarge(t *testing.T) {
	assert := assert.New(t)

	builtMsg := Txs{
		Txs: utils.RandomBytes(1024 * units.KiB),
	}
	codec, err := BuildCodec()
	assert.NoError(err)
	_, err = BuildMessage(codec, &builtMsg)
	assert.Error(err)
}

func TestParseGibberish(t *testing.T) {
	assert := assert.New(t)

	codec, err := BuildCodec()
	assert.NoError(err)
	randomBytes := utils.RandomBytes(256 * units.KiB)
	_, err = ParseMessage(codec, randomBytes)
	assert.Error(err)
}
