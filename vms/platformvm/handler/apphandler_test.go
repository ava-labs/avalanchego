// (c) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/stretchr/testify/require"
)

type CounterHandler struct {
	TxGossip int
}

func (h *CounterHandler) HandleTx(ids.NodeID, *message.TxGossip) error {
	h.TxGossip++
	return nil
}

func TestHandleTx(t *testing.T) {
	require := require.New(t)

	handler := CounterHandler{}
	msg := message.TxGossip{}

	err := msg.Handle(&handler, ids.EmptyNodeID, 0)
	require.NoError(err)
	require.Equal(1, handler.TxGossip)
}
