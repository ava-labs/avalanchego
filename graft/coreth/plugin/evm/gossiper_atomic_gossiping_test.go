// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/stretchr/testify/assert"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/coreth/plugin/evm/message"
)

// show that a txID discovered from gossip is requested to the same node only if
// the txID is unknown
func TestMempoolAtmTxsAppGossipHandling(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, "", "", "")
	defer func() {
		assert.NoError(vm.Shutdown(context.Background()))
	}()

	nodeID := ids.GenerateTestNodeID()

	var (
		txGossiped     int
		txGossipedLock sync.Mutex
		txRequested    bool
	)
	sender.CantSendAppGossip = false
	sender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error {
		txGossipedLock.Lock()
		defer txGossipedLock.Unlock()

		txGossiped++
		return nil
	}
	sender.SendAppRequestF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
		txRequested = true
		return nil
	}

	// Create conflicting transactions
	importTxs := createImportTxOptions(t, vm, sharedMemory)
	tx, conflictingTx := importTxs[0], importTxs[1]

	// gossip tx and check it is accepted and gossiped
	msg := message.AtomicTxGossip{
		Tx: tx.SignedBytes(),
	}
	msgBytes, err := message.BuildGossipMessage(vm.networkCodec, msg)
	assert.NoError(err)

	vm.ctx.Lock.Unlock()

	// show that no txID is requested
	assert.NoError(vm.AppGossip(context.Background(), nodeID, msgBytes))
	time.Sleep(500 * time.Millisecond)

	vm.ctx.Lock.Lock()

	assert.False(txRequested, "tx should not have been requested")
	txGossipedLock.Lock()
	assert.Equal(0, txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()
	assert.True(vm.mempool.has(tx.ID()))

	vm.ctx.Lock.Unlock()

	// show that tx is not re-gossiped
	assert.NoError(vm.AppGossip(context.Background(), nodeID, msgBytes))

	vm.ctx.Lock.Lock()

	txGossipedLock.Lock()
	assert.Equal(0, txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()

	// show that conflicting tx is not added to mempool
	msg = message.AtomicTxGossip{
		Tx: conflictingTx.SignedBytes(),
	}
	msgBytes, err = message.BuildGossipMessage(vm.networkCodec, msg)
	assert.NoError(err)

	vm.ctx.Lock.Unlock()

	assert.NoError(vm.AppGossip(context.Background(), nodeID, msgBytes))

	vm.ctx.Lock.Lock()

	assert.False(txRequested, "tx should not have been requested")
	txGossipedLock.Lock()
	assert.Equal(0, txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()
	assert.False(vm.mempool.has(conflictingTx.ID()), "conflicting tx should not be in the atomic mempool")
}

// show that txs already marked as invalid are not re-requested on gossiping
func TestMempoolAtmTxsAppGossipHandlingDiscardedTx(t *testing.T) {
	if os.Getenv("RUN_FLAKY_TESTS") != "true" {
		t.Skip("FLAKY")
	}
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, "", "", "")
	defer func() {
		assert.NoError(vm.Shutdown(context.Background()))
	}()
	mempool := vm.mempool

	var (
		txGossiped     int
		txGossipedLock sync.Mutex
		txRequested    bool
	)
	sender.CantSendAppGossip = false
	sender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error {
		txGossipedLock.Lock()
		defer txGossipedLock.Unlock()

		txGossiped++
		return nil
	}
	sender.SendAppRequestF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
		txRequested = true
		return nil
	}

	// Create a transaction and mark it as invalid by discarding it
	importTxs := createImportTxOptions(t, vm, sharedMemory)
	tx, conflictingTx := importTxs[0], importTxs[1]
	txID := tx.ID()

	mempool.AddTx(tx)
	mempool.NextTx()
	mempool.DiscardCurrentTx(txID)

	// Check the mempool does not contain the discarded transaction
	assert.False(mempool.has(txID))

	// Gossip the transaction to the VM and ensure that it is not added to the mempool
	// and is not re-gossipped.
	nodeID := ids.GenerateTestNodeID()
	msg := message.AtomicTxGossip{
		Tx: tx.SignedBytes(),
	}
	msgBytes, err := message.BuildGossipMessage(vm.networkCodec, msg)
	assert.NoError(err)

	vm.ctx.Lock.Unlock()

	assert.NoError(vm.AppGossip(context.Background(), nodeID, msgBytes))

	vm.ctx.Lock.Lock()

	assert.False(txRequested, "tx shouldn't be requested")
	txGossipedLock.Lock()
	assert.Zero(txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()

	assert.False(mempool.has(txID))

	// Gossip the transaction that conflicts with the originally
	// discarded tx and ensure it is accepted into the mempool and gossipped
	// to the network.
	nodeID = ids.GenerateTestNodeID()
	msg = message.AtomicTxGossip{
		Tx: conflictingTx.SignedBytes(),
	}
	msgBytes, err = message.BuildGossipMessage(vm.networkCodec, msg)
	assert.NoError(err)

	vm.ctx.Lock.Unlock()

	assert.NoError(vm.AppGossip(context.Background(), nodeID, msgBytes))
	time.Sleep(500 * time.Millisecond)

	vm.ctx.Lock.Lock()

	assert.False(txRequested, "tx shouldn't be requested")
	txGossipedLock.Lock()
	assert.Equal(1, txGossiped, "conflicting tx should have been gossiped")
	txGossipedLock.Unlock()

	assert.False(mempool.has(txID))
	assert.True(mempool.has(conflictingTx.ID()))
}
