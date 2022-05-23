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

func (h *CounterHandler) HandleTxs(ids.NodeID, TxsGossip) error {
	h.Txs++
	return nil
}

func TestHandleTxs(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := TxsGossip{}

	err := msg.Handle(&handler, ids.EmptyNodeID)
	assert.NoError(err)
	assert.Equal(1, handler.Txs)
}

func TestNoopHandler(t *testing.T) {
	assert := assert.New(t)

	handler := NoopMempoolGossipHandler{}

	err := handler.HandleTxs(ids.EmptyNodeID, TxsGossip{})
	assert.NoError(err)
}
