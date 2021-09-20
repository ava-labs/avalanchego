// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/stretchr/testify/assert"
)

type CounterHandler struct {
	AtomicTx, EthTxs int
}

func (h *CounterHandler) HandleAtomicTx(ids.ShortID, uint32, *AtomicTx) error {
	h.AtomicTx++
	return nil
}

func (h *CounterHandler) HandleEthTxs(ids.ShortID, uint32, *EthTxs) error {
	h.EthTxs++
	return nil
}

func TestHandleAtomicTx(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := AtomicTx{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Equal(1, handler.AtomicTx)
	assert.Zero(handler.EthTxs)
}

func TestHandleEthTxs(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := EthTxs{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Zero(handler.AtomicTx)
	assert.Equal(1, handler.EthTxs)
}

func TestNoopHandler(t *testing.T) {
	assert := assert.New(t)

	handler := NoopHandler{}

	err := handler.HandleAtomicTx(ids.ShortEmpty, 0, nil)
	assert.NoError(err)

	err = handler.HandleEthTxs(ids.ShortEmpty, 0, nil)
	assert.NoError(err)
}
