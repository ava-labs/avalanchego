// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/platformvm/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/builder"
	"github.com/stretchr/testify/assert"
)

func getValidTx(txBuilder builder.Builder, t *testing.T) *txs.Tx {
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
	assert := assert.New(t)

	h := newTestHelpersCollection(t, false /*mockResetBlockTimer*/)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.BlockBuilder.SetActivationTime(time.Unix(0, 0)) // enable mempool gossiping
	h.ctx.Lock.Lock()

	var gossipedBytes []byte
	h.sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	nodeID := ids.GenerateTestNodeID()

	// create a tx
	tx := getValidTx(h.txBuilder, t)
	txID := tx.ID()

	msg := message.Tx{Tx: tx.Bytes()}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)
	// Free lock because [AppGossip] waits for the context lock
	h.ctx.Lock.Unlock()
	// show that unknown tx is added to mempool
	err = h.AppGossip(nodeID, msgBytes)
	assert.NoError(err, "error in reception of gossiped tx")
	assert.True(h.BlockBuilder.Has(txID))
	// Grab lock back
	h.ctx.Lock.Lock()

	// and gossiped if it has just been discovered
	assert.True(gossipedBytes != nil)

	// show gossiped bytes can be decoded to the original tx
	replyIntf, err := message.Parse(gossipedBytes)
	assert.NoError(err, "failed to parse gossip")

	reply, ok := replyIntf.(*message.Tx)
	assert.True(ok, "unknown message type")

	retrivedTx, err := txs.Parse(txs.Codec, reply.Tx)
	assert.NoError(err, "failed parsing tx")

	assert.Equal(txID, retrivedTx.ID())
}

// show that txs already marked as invalid are not re-requested on gossiping
func TestMempoolInvalidGossipedTxIsNotAddedToMempool(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t, false /*mockResetBlockTimer*/)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.BlockBuilder.SetActivationTime(time.Unix(0, 0)) // enable mempool gossiping
	h.ctx.Lock.Lock()

	// create a tx and mark as invalid
	tx := getValidTx(h.txBuilder, t)
	txID := tx.ID()
	h.BlockBuilder.MarkDropped(txID, "dropped for testing")

	// show that the invalid tx is not requested
	nodeID := ids.GenerateTestNodeID()
	msg := message.Tx{Tx: tx.Bytes()}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)
	h.ctx.Lock.Unlock()
	err = h.AppGossip(nodeID, msgBytes)
	h.ctx.Lock.Lock()
	assert.NoError(err, "error in reception of gossiped tx")
	assert.False(h.BlockBuilder.Has(txID))
}

// show that locally generated txs are gossiped
func TestMempoolNewLocaTxIsGossiped(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t, false /*mockResetBlockTimer*/)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.BlockBuilder.SetActivationTime(time.Unix(0, 0)) // enable mempool gossiping
	h.ctx.Lock.Lock()

	var gossipedBytes []byte
	h.sender.SendAppGossipF = func(b []byte) error {
		gossipedBytes = b
		return nil
	}

	// add a tx to the mempool and show it gets gossiped
	tx := getValidTx(h.txBuilder, t)
	txID := tx.ID()

	err := h.BlockBuilder.AddUnverifiedTx(tx)
	assert.NoError(err, "couldn't add tx to mempool")
	assert.True(gossipedBytes != nil)

	// show gossiped bytes can be decoded to the original tx
	replyIntf, err := message.Parse(gossipedBytes)
	assert.NoError(err, "failed to parse gossip")

	reply, ok := replyIntf.(*message.Tx)
	assert.True(ok, "unknown message type")

	retrivedTx, err := txs.Parse(txs.Codec, reply.Tx)
	assert.NoError(err, "failed parsing tx")

	assert.Equal(txID, retrivedTx.ID())

	// show that transaction is not re-gossiped is recently added to mempool
	gossipedBytes = nil
	h.BlockBuilder.RemoveDecisionTxs([]*txs.Tx{tx})
	err = h.BlockBuilder.Add(tx)
	assert.NoError(err, "could not reintroduce tx to mempool")

	assert.True(gossipedBytes == nil)
}
