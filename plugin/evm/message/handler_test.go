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

func (h *CounterHandler) HandleEthTxs(ids.NodeID, EthTxsGossip) error {
	h.EthTxs++
	return nil
}

func TestHandleEthTxs(t *testing.T) {
	assert := assert.New(t)

	handler := CounterHandler{}
	msg := EthTxsGossip{}

	err := msg.Handle(&handler, ids.EmptyNodeID)
	assert.NoError(err)
	assert.Equal(1, handler.EthTxs)
}

func TestNoopHandler(t *testing.T) {
	assert := assert.New(t)

	handler := NoopMempoolGossipHandler{}

	err := handler.HandleEthTxs(ids.EmptyNodeID, EthTxsGossip{})
	assert.NoError(err)
}
