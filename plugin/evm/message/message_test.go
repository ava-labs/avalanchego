// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"encoding/base64"
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/stretchr/testify/assert"
)

// TestMarshalTxs asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalTxs(t *testing.T) {
	assert := assert.New(t)

	base64EthTxGossip := "AAAAAAAAAAAABGJsYWg="
	msg := []byte("blah")
	builtMsg := TxsGossip{
		Txs: msg,
	}
	codec, err := BuildCodec()
	assert.NoError(err)
	builtMsgBytes, err := BuildGossipMessage(codec, builtMsg)
	assert.NoError(err)
	assert.Equal(base64EthTxGossip, base64.StdEncoding.EncodeToString(builtMsgBytes))

	parsedMsgIntf, err := ParseGossipMessage(codec, builtMsgBytes)
	assert.NoError(err)

	parsedMsg, ok := parsedMsgIntf.(TxsGossip)
	assert.True(ok)

	assert.Equal(msg, parsedMsg.Txs)
}

func TestTxsTooLarge(t *testing.T) {
	assert := assert.New(t)

	builtMsg := TxsGossip{
		Txs: utils.RandomBytes(1024 * units.KiB),
	}
	codec, err := BuildCodec()
	assert.NoError(err)
	_, err = BuildGossipMessage(codec, builtMsg)
	assert.Error(err)
}

func TestParseGibberish(t *testing.T) {
	assert := assert.New(t)

	codec, err := BuildCodec()
	assert.NoError(err)
	randomBytes := utils.RandomBytes(256 * units.KiB)
	_, err = ParseGossipMessage(codec, randomBytes)
	assert.Error(err)
}
