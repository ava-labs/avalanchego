// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/units"

	"github.com/stretchr/testify/assert"
)

func TestAtomicTxNotify(t *testing.T) {
	assert := assert.New(t)

	txID := ids.GenerateTestID()
	builtMsg := AtomicTxNotify{
		TxID: txID,
	}
	builtMsgBytes, err := Build(&builtMsg)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, builtMsg.Bytes())

	parsedMsgIntf, err := Parse(builtMsgBytes)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, parsedMsgIntf.Bytes())

	parsedMsg, ok := parsedMsgIntf.(*AtomicTxNotify)
	assert.True(ok)

	assert.Equal(txID, parsedMsg.TxID)
}

func TestAtomicTx(t *testing.T) {
	assert := assert.New(t)

	tx := utils.RandomBytes(256 * units.KiB)
	builtMsg := AtomicTx{
		Tx: tx,
	}
	builtMsgBytes, err := Build(&builtMsg)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, builtMsg.Bytes())

	parsedMsgIntf, err := Parse(builtMsgBytes)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, parsedMsgIntf.Bytes())

	parsedMsg, ok := parsedMsgIntf.(*AtomicTx)
	assert.True(ok)

	assert.Equal(tx, parsedMsg.Tx)
}

func TestEthTxsNotify(t *testing.T) {
	assert := assert.New(t)

	txs := make([]EthTxNotify, MaxEthTxsLen)
	builtMsg := EthTxsNotify{
		Txs: txs,
	}
	builtMsgBytes, err := Build(&builtMsg)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, builtMsg.Bytes())

	parsedMsgIntf, err := Parse(builtMsgBytes)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, parsedMsgIntf.Bytes())

	parsedMsg, ok := parsedMsgIntf.(*EthTxsNotify)
	assert.True(ok)

	assert.Equal(txs, parsedMsg.Txs)
}

func TestEthTxsNotifyTooLarge(t *testing.T) {
	assert := assert.New(t)

	txs := make([]EthTxNotify, MaxEthTxsLen+1)
	builtMsg := EthTxsNotify{
		Txs: txs,
	}
	_, err := Build(&builtMsg)
	assert.Error(err)
}

func TestEthTxs(t *testing.T) {
	assert := assert.New(t)

	txs := utils.RandomBytes(256 * units.KiB)
	builtMsg := EthTxs{
		TxsBytes: txs,
	}
	builtMsgBytes, err := Build(&builtMsg)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, builtMsg.Bytes())

	parsedMsgIntf, err := Parse(builtMsgBytes)
	assert.NoError(err)
	assert.Equal(builtMsgBytes, parsedMsgIntf.Bytes())

	parsedMsg, ok := parsedMsgIntf.(*EthTxs)
	assert.True(ok)

	assert.Equal(txs, parsedMsg.TxsBytes)
}

func TestParseGibberish(t *testing.T) {
	assert := assert.New(t)

	randomBytes := utils.RandomBytes(256 * units.KiB)
	_, err := Parse(randomBytes)
	assert.Error(err)
}
