// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

import (
	"math"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/timed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
	"github.com/stretchr/testify/assert"
)

// shows that a locally generated CreateChainTx can be added to mempool and then
// removed by inclusion in a block
func TestBlockBuilderAddLocalTx(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.BlockBuilder.SetActivationTime(time.Unix(0, 0)) // enable mempool gossiping

	// add a tx to it
	tx := getValidTx(h.txBuilder, t)
	txID := tx.ID()

	h.sender.SendAppGossipF = func(b []byte) error { return nil }
	err := h.BlockBuilder.AddUnverifiedTx(tx)
	assert.NoError(err, "couldn't add tx to mempool")

	has := h.mpool.Has(txID)
	assert.True(has, "valid tx not recorded into mempool")

	// show that build block include that tx and removes it from mempool
	blkIntf, err := h.BlockBuilder.BuildBlock()
	assert.NoError(err, "couldn't build block out of mempool")

	blk, ok := blkIntf.(*StandardBlock)
	assert.True(ok, "expected standard block")
	assert.Len(blk.DecisionTxs(), 1, "standard block should include a single transaction")
	assert.Equal(txID, blk.DecisionTxs()[0].ID(), "standard block does not include expected transaction")

	has = h.mpool.Has(txID)
	assert.False(has, "tx included in block is still recorded into mempool")
}

func TestPreviouslyDroppedTxsCanBeReAddedToMempool(t *testing.T) {
	assert := assert.New(t)

	h := newTestHelpersCollection(t)
	defer func() {
		if err := internalStateShutdown(h); err != nil {
			t.Fatal(err)
		}
	}()
	h.BlockBuilder.SetActivationTime(time.Unix(0, 0)) // enable mempool gossiping

	// create candidate tx
	tx := getValidTx(h.txBuilder, t)
	txID := tx.ID()

	// A tx simply added to mempool is obviously not marked as dropped
	assert.NoError(h.mpool.Add(tx))
	assert.True(h.mpool.Has(txID))
	_, isDropped := h.mpool.GetDropReason(txID)
	assert.False(isDropped)

	// When a tx is marked as dropped, it is still available to allow re-issuance
	h.mpool.MarkDropped(txID, "dropped for testing")
	assert.True(h.mpool.Has(txID)) // still available
	_, isDropped = h.mpool.GetDropReason(txID)
	assert.True(isDropped)

	// A previously dropped tx, popped then re-added to mempool,
	// is not dropped anymore
	switch tx.Unsigned.(type) {
	case timed.Tx:
		h.mpool.PopProposalTx()
	case *unsigned.CreateChainTx,
		*unsigned.CreateSubnetTx,
		*unsigned.ImportTx,
		*unsigned.ExportTx:
		h.mpool.PopDecisionTxs(math.MaxInt64)
	default:
		t.Fatal("unknown tx type")
	}
	assert.NoError(h.mpool.Add(tx))

	assert.True(h.mpool.Has(txID))
	_, isDropped = h.mpool.GetDropReason(txID)
	assert.False(isDropped)
}
