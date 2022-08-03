// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/stretchr/testify/assert"
)

// shows that a locally generated CreateChainTx can be added to mempool and then
// removed by inclusion in a block
func TestBlockBuilderAddLocalTx(t *testing.T) {
	assert := assert.New(t)

	env := newEnvironment(t)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	// add a tx to it
	tx := getValidTx(env.txBuilder, t)
	txID := tx.ID()

	env.sender.SendAppGossipF = func(b []byte) error { return nil }
	err := env.BlockBuilder.AddUnverifiedTx(tx)
	assert.NoError(err, "couldn't add tx to mempool")

	has := env.mpool.Has(txID)
	assert.True(has, "valid tx not recorded into mempool")

	// show that build block include that tx and removes it from mempool
	blkIntf, err := env.BlockBuilder.BuildBlock()
	assert.NoError(err, "couldn't build block out of mempool")

	blk, ok := blkIntf.(*executor.Block)
	assert.True(ok, "expected standard block")
	assert.Len(blk.Txs(), 1, "standard block should include a single transaction")
	assert.Equal(txID, blk.Txs()[0].ID(), "standard block does not include expected transaction")

	has = env.mpool.Has(txID)
	assert.False(has, "tx included in block is still recorded into mempool")
}

func TestPreviouslyDroppedTxsCanBeReAddedToMempool(t *testing.T) {
	assert := assert.New(t)

	env := newEnvironment(t)
	defer func() {
		if err := shutdownEnvironment(env); err != nil {
			t.Fatal(err)
		}
	}()

	// create candidate tx
	tx := getValidTx(env.txBuilder, t)
	txID := tx.ID()

	// A tx simply added to mempool is obviously not marked as dropped
	assert.NoError(env.mpool.Add(tx))
	assert.True(env.mpool.Has(txID))
	_, isDropped := env.mpool.GetDropReason(txID)
	assert.False(isDropped)

	// When a tx is marked as dropped, it is still available to allow re-issuance
	env.mpool.MarkDropped(txID, "dropped for testing")
	assert.True(env.mpool.Has(txID)) // still available
	_, isDropped = env.mpool.GetDropReason(txID)
	assert.True(isDropped)

	// A previously dropped tx, popped then re-added to mempool,
	// is not dropped anymore
	switch tx.Unsigned.(type) {
	case txs.StakerTx:
		env.mpool.PopProposalTx()
	case *txs.CreateChainTx,
		*txs.CreateSubnetTx,
		*txs.ImportTx,
		*txs.ExportTx:
		env.mpool.PopDecisionTxs(math.MaxInt64)
	default:
		t.Fatal("unknown tx type")
	}
	assert.NoError(env.mpool.Add(tx))

	assert.True(env.mpool.Has(txID))
	_, isDropped = env.mpool.GetDropReason(txID)
	assert.False(isDropped)
}
