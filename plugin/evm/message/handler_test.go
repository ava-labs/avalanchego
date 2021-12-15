// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"

	"github.com/stretchr/testify/assert"
)

type CounterHandler struct {
	EthTxs int
}

func (h *CounterHandler) HandleTxs(ids.ShortID, uint32, *Txs) error {
	h.EthTxs++
	return nil
}

func TestHandleTxs(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := Txs{}

	err := msg.Handle(&handler, ids.ShortEmpty, 0)
	assert.NoError(err)
	assert.Equal(1, handler.EthTxs)
}

func TestNoopHandler(t *testing.T) {
	assert := assert.New(t)

	handler := NoopHandler{}

	err := handler.HandleTxs(ids.ShortEmpty, 0, nil)
	assert.NoError(err)
}
