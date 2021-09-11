// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/stretchr/testify/assert"
)

type CounterHandler struct {
	AtomicTxNotify, EthTxsNotify int
}

func (h *CounterHandler) HandleAtomicTxNotify(ids.ShortID, uint32, *AtomicTxNotify) error {
	h.AtomicTxNotify++
	return nil
}

func (h *CounterHandler) HandleEthTxsNotify(ids.ShortID, uint32, *EthTxsNotify) error {
	h.EthTxsNotify++
	return nil
}

func TestHandleAtomicTxNotify(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := AtomicTxNotify{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Equal(1, handler.AtomicTxNotify)
	assert.Zero(handler.EthTxsNotify)
}

func TestHandleEthTxsNotify(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := EthTxsNotify{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Zero(handler.AtomicTxNotify)
	assert.Equal(1, handler.EthTxsNotify)
}

func TestNoopHandler(t *testing.T) {
	assert := assert.New(t)

	handler := NoopHandler{}

	err := handler.HandleAtomicTxNotify(ids.ShortEmpty, 0, nil)
	assert.NoError(err)

	err = handler.HandleEthTxsNotify(ids.ShortEmpty, 0, nil)
	assert.NoError(err)
}
