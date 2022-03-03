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

// TestMarshalAtomicTx asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalAtomicTx(t *testing.T) {
	assert := assert.New(t)

	base64AtomicTxGossip := "AAAAAAAAAAAABGJsYWg="
	msg := []byte("blah")
	builtMsg := AtomicTxGossip{
		Tx: msg,
	}
	codec, err := BuildCodec()
	assert.NoError(err)
	builtMsgBytes, err := BuildGossipMessage(codec, builtMsg)
	assert.NoError(err)
	assert.Equal(base64AtomicTxGossip, base64.StdEncoding.EncodeToString(builtMsgBytes))

	parsedMsgIntf, err := ParseGossipMessage(codec, builtMsgBytes)
	assert.NoError(err)

	parsedMsg, ok := parsedMsgIntf.(AtomicTxGossip)
	assert.True(ok)

	assert.Equal(msg, parsedMsg.Tx)
}

// TestMarshalEthTxs asserts that the structure or serialization logic hasn't changed, primarily to
// ensure compatibility with the network.
func TestMarshalEthTxs(t *testing.T) {
	assert := assert.New(t)

	base64EthTxGossip := "AAAAAAABAAAABGJsYWg="
	msg := []byte("blah")
	builtMsg := EthTxsGossip{
		Txs: msg,
	}
	codec, err := BuildCodec()
	assert.NoError(err)
	builtMsgBytes, err := BuildGossipMessage(codec, builtMsg)
	assert.NoError(err)
	assert.Equal(base64EthTxGossip, base64.StdEncoding.EncodeToString(builtMsgBytes))

	parsedMsgIntf, err := ParseGossipMessage(codec, builtMsgBytes)
	assert.NoError(err)

	parsedMsg, ok := parsedMsgIntf.(EthTxsGossip)
	assert.True(ok)

	assert.Equal(msg, parsedMsg.Txs)
}

func TestEthTxsTooLarge(t *testing.T) {
	assert := assert.New(t)

	builtMsg := EthTxsGossip{
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
