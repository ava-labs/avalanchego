// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/stretchr/testify/assert"
)

type CounterHandler struct {
	Txs int
}

func (h *CounterHandler) HandleTxs(ids.ShortID, *Txs) error {
	h.Txs++
	return nil
}

func TestHandleTxs(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := Txs{}

	err := msg.Handle(&handler, ids.ShortEmpty)
	assert.NoError(err)
	assert.Equal(1, handler.Txs)
}

func TestNoopHandler(t *testing.T) {
	assert := assert.New(t)

	handler := NoopMempoolGossipHandler{}

	err := handler.HandleTxs(ids.ShortEmpty, nil)
	assert.NoError(err)
}
