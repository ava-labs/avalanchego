// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"

	"github.com/stretchr/testify/assert"
)

type CounterHandler struct {
	TxNotify, Tx int
}

func (h *CounterHandler) HandleTxNotify(ids.ShortID, uint32, *TxNotify) error {
	h.TxNotify++
	return nil
}

func (h *CounterHandler) HandleTx(ids.ShortID, uint32, *Tx) error {
	h.Tx++
	return nil
}

func TestHandleTxNotify(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := TxNotify{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Equal(1, handler.TxNotify)
	assert.Zero(handler.Tx)
}

func TestHandleTx(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := Tx{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Zero(handler.TxNotify)
	assert.Equal(1, handler.Tx)
}

func TestNoopHandler(t *testing.T) {
	assert := assert.New(t)

	handler := NoopHandler{
		Log: logging.NoLog{},
	}

	err := handler.HandleTxNotify(ids.ShortEmpty, 0, nil)
	assert.NoError(err)

	err = handler.HandleTx(ids.ShortEmpty, 0, nil)
	assert.NoError(err)
}
