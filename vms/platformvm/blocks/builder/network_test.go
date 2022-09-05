// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

	txbuilder "github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
)

func getValidTx(txBuilder txbuilder.Builder, t *testing.T) *txs.Tx {
	tx, err := txBuilder.NewCreateChainTx(
		testSubnet1.ID(),
		nil,
		constants.AVMID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty,
	)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

// show that a tx learned from gossip is validated and added to mempool
func TestMempoolValidGossipedTxIsAddedToMempool(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	var gossipedBytes []byte
	env.sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	nodeID := ids.GenerateTestNodeID()

	// create a tx
	tx := getValidTx(env.txBuilder, t)
	txID := tx.ID()

	msg := message.Tx{Tx: tx.Bytes()}
	msgBytes, err := message.Build(&msg)
	require.NoError(err)
	// Free lock because [AppGossip] waits for the context lock
	env.ctx.Lock.Unlock()
	// show that unknown tx is added to mempool
	err = env.AppGossip(nodeID, msgBytes)
	require.NoError(err, "error in reception of gossiped tx")
	require.True(env.Builder.Has(txID))
	// Grab lock back
	env.ctx.Lock.Lock()

	// and gossiped if it has just been discovered
	require.True(gossipedBytes != nil)

	// show gossiped bytes can be decoded to the original tx
	replyIntf, err := message.Parse(gossipedBytes)
	require.NoError(err, "failed to parse gossip")

	reply, ok := replyIntf.(*message.Tx)
	require.True(ok, "unknown message type")

	retrivedTx, err := txs.Parse(txs.Codec, reply.Tx)
	require.NoError(err, "failed parsing tx")

	require.Equal(txID, retrivedTx.ID())
}

// show that txs already marked as invalid are not re-requested on gossiping
func TestMempoolInvalidGossipedTxIsNotAddedToMempool(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	// create a tx and mark as invalid
	tx := getValidTx(env.txBuilder, t)
	txID := tx.ID()
	env.Builder.MarkDropped(txID, "dropped for testing")

	// show that the invalid tx is not requested
	nodeID := ids.GenerateTestNodeID()
	msg := message.Tx{Tx: tx.Bytes()}
	msgBytes, err := message.Build(&msg)
	require.NoError(err)
	env.ctx.Lock.Unlock()
	err = env.AppGossip(nodeID, msgBytes)
	env.ctx.Lock.Lock()
	require.NoError(err, "error in reception of gossiped tx")
	require.False(env.Builder.Has(txID))
}

// show that locally generated txs are gossiped
func TestMempoolNewLocaTxIsGossiped(t *testing.T) {
	require := require.New(t)

	env := newEnvironment(t)
	env.ctx.Lock.Lock()
	defer func() {
		require.NoError(shutdownEnvironment(env))
	}()

	var gossipedBytes []byte
	env.sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	// add a tx to the mempool and show it gets gossiped
	tx := getValidTx(env.txBuilder, t)
	txID := tx.ID()

	err := env.Builder.AddUnverifiedTx(tx)
	require.NoError(err, "couldn't add tx to mempool")
	require.True(gossipedBytes != nil)

	// show gossiped bytes can be decoded to the original tx
	replyIntf, err := message.Parse(gossipedBytes)
	require.NoError(err, "failed to parse gossip")

	reply, ok := replyIntf.(*message.Tx)
	require.True(ok, "unknown message type")

	retrivedTx, err := txs.Parse(txs.Codec, reply.Tx)
	require.NoError(err, "failed parsing tx")

	require.Equal(txID, retrivedTx.ID())

	// show that transaction is not re-gossiped is recently added to mempool
	gossipedBytes = nil
	env.Builder.Remove([]*txs.Tx{tx})
	err = env.Builder.Add(tx)
	require.NoError(err, "could not reintroduce tx to mempool")

	require.True(gossipedBytes == nil)
}
