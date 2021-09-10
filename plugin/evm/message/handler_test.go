// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/stretchr/testify/assert"
)

type CounterHandler struct {
	AtomicTxNotify, AtomicTxRequest, AtomicTx, EthTxsNotify, EthTxsRequest, EthTxs int
}

func (h *CounterHandler) HandleAtomicTxNotify(ids.ShortID, uint32, *AtomicTxNotify) error {
	h.AtomicTxNotify++
	return nil
}

func (h *CounterHandler) HandleAtomicTxRequest(ids.ShortID, uint32, *AtomicTxRequest) error {
	h.AtomicTxRequest++
	return nil
}

func (h *CounterHandler) HandleAtomicTx(ids.ShortID, uint32, *AtomicTx) error {
	h.AtomicTx++
	return nil
}

func (h *CounterHandler) HandleEthTxsNotify(ids.ShortID, uint32, *EthTxsNotify) error {
	h.EthTxsNotify++
	return nil
}

func (h *CounterHandler) HandleEthTxsRequest(ids.ShortID, uint32, *EthTxsRequest) error {
	h.EthTxsRequest++
	return nil
}

func (h *CounterHandler) HandleEthTxs(ids.ShortID, uint32, *EthTxs) error {
	h.EthTxs++
	return nil
}

func TestHandleAtomicTxNotify(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := AtomicTxNotify{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Equal(1, handler.AtomicTxNotify)
	assert.Zero(handler.AtomicTxRequest)
	assert.Zero(handler.AtomicTx)
	assert.Zero(handler.EthTxsNotify)
	assert.Zero(handler.EthTxsRequest)
	assert.Zero(handler.EthTxs)
}

func TestHandleAtomicTxRequest(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := AtomicTxRequest{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Zero(handler.AtomicTxNotify)
	assert.Equal(1, handler.AtomicTxRequest)
	assert.Zero(handler.AtomicTx)
	assert.Zero(handler.EthTxsNotify)
	assert.Zero(handler.EthTxsRequest)
	assert.Zero(handler.EthTxs)
}

func TestHandleAtomicTx(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := AtomicTx{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Zero(handler.AtomicTxNotify)
	assert.Zero(handler.AtomicTxRequest)
	assert.Equal(1, handler.AtomicTx)
	assert.Zero(handler.EthTxsNotify)
	assert.Zero(handler.EthTxsRequest)
	assert.Zero(handler.EthTxs)
}

func TestHandleEthTxsNotify(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := EthTxsNotify{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Zero(handler.AtomicTxNotify)
	assert.Zero(handler.AtomicTxRequest)
	assert.Zero(handler.AtomicTx)
	assert.Equal(1, handler.EthTxsNotify)
	assert.Zero(handler.EthTxsRequest)
	assert.Zero(handler.EthTxs)
}

func TestHandleEthTxsRequest(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := EthTxsRequest{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Zero(handler.AtomicTxNotify)
	assert.Zero(handler.AtomicTxRequest)
	assert.Zero(handler.AtomicTx)
	assert.Zero(handler.EthTxsNotify)
	assert.Equal(1, handler.EthTxsRequest)
	assert.Zero(handler.EthTxs)
}

func TestHandleEthTxs(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := EthTxs{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Zero(handler.AtomicTxNotify)
	assert.Zero(handler.AtomicTxRequest)
	assert.Zero(handler.AtomicTx)
	assert.Zero(handler.EthTxsNotify)
	assert.Zero(handler.EthTxsRequest)
	assert.Equal(1, handler.EthTxs)
}

func TestNoopHandler(t *testing.T) {
	assert := assert.New(t)

	handler := NoopHandler{}

	err := handler.HandleAtomicTxNotify(ids.ShortEmpty, 0, nil)
	assert.NoError(err)

	err = handler.HandleAtomicTxRequest(ids.ShortEmpty, 0, nil)
	assert.NoError(err)

	err = handler.HandleAtomicTx(ids.ShortEmpty, 0, nil)
	assert.NoError(err)

	err = handler.HandleEthTxsNotify(ids.ShortEmpty, 0, nil)
	assert.NoError(err)

	err = handler.HandleEthTxsRequest(ids.ShortEmpty, 0, nil)
	assert.NoError(err)

	err = handler.HandleEthTxs(ids.ShortEmpty, 0, nil)
	assert.NoError(err)
}
