// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type CounterHandler struct {
	Tx int
}

func (h *CounterHandler) HandleTx(ids.NodeID, uint32, *Tx) error {
	h.Tx++
	return nil
}

func TestHandleTx(t *testing.T) {
	require := require.New(t)

	handler := CounterHandler{}
	msg := Tx{}

	err := msg.Handle(&handler, ids.EmptyNodeID, 0)
	require.NoError(err)
	require.Equal(1, handler.Tx)
}

func TestNoopHandler(t *testing.T) {
	handler := NoopHandler{
		Log: logging.NoLog{},
	}

	err := handler.HandleTx(ids.EmptyNodeID, 0, nil)
	require.NoError(t, err)
}
