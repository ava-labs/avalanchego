// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/builder"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/stretchr/testify/require"
)

func TestMempoolValidGossipedTxIsAddedToMempool(t *testing.T) {
	require := require.New(t)

	env := builder.NewEnvironment(t)
	defer func() {
		require.NoError(env.Shutdown())
	}()

	nodeID := ids.GenerateTestNodeID()

	// create a tx
	tx := env.GetValidTx(t)
	txID := tx.ID()

	msg := message.TxGossip{Tx: tx.Bytes()}
	// show that unknown tx is added to mempool
	gossipHandler := NewGossipHandler(env.Ctx, env.Builder)

	err := gossipHandler.HandleTx(nodeID, &msg)
	require.NoError(err)
	require.True(env.Builder.Has(txID))
}

func TestMempoolInvalidTxIsNotAddedToMempool(t *testing.T) {
	require := require.New(t)

	env := builder.NewEnvironment(t)

	defer func() {
		require.NoError(env.Shutdown())
	}()

	// create a tx and mark as invalid
	tx := env.GetValidTx(t)
	txID := tx.ID()
	env.Builder.MarkDropped(txID, "dropped for testing")

	gossipHandler := NewGossipHandler(env.Ctx, env.Builder)
	msg := message.TxGossip{Tx: tx.Bytes()}
	// show that the invalid tx is not added
	err := gossipHandler.HandleTx(ids.GenerateTestNodeID(), &msg)
	require.NoError(err)

	require.False(env.Builder.Has(txID))
}
