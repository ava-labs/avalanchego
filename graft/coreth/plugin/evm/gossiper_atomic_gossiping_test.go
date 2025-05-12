// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"

	"github.com/ava-labs/coreth/plugin/evm/atomic"
)

// show that a txID discovered from gossip is requested to the same node only if
// the txID is unknown
func TestMempoolAtmTxsAppGossipHandling(t *testing.T) {
	assert := assert.New(t)

	tvm := newVM(t, testVMConfig{})
	defer func() {
		assert.NoError(tvm.vm.Shutdown(context.Background()))
	}()

	nodeID := ids.GenerateTestNodeID()

	var (
		txGossiped     int
		txGossipedLock sync.Mutex
		txRequested    bool
	)
	tvm.appSender.CantSendAppGossip = false
	tvm.appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error {
		txGossipedLock.Lock()
		defer txGossipedLock.Unlock()

		txGossiped++
		return nil
	}
	tvm.appSender.SendAppRequestF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
		txRequested = true
		return nil
	}

	// Create conflicting transactions
	importTxs := createImportTxOptions(t, tvm.vm, tvm.atomicMemory)
	tx, conflictingTx := importTxs[0], importTxs[1]

	// gossip tx and check it is accepted and gossiped
	msg := atomic.GossipAtomicTx{
		Tx: tx,
	}
	marshaller := atomic.GossipAtomicTxMarshaller{}
	txBytes, err := marshaller.MarshalGossip(&msg)
	assert.NoError(err)
	tvm.vm.ctx.Lock.Unlock()

	msgBytes, err := buildAtomicPushGossip(txBytes)
	assert.NoError(err)

	// show that no txID is requested
	assert.NoError(tvm.vm.AppGossip(context.Background(), nodeID, msgBytes))
	time.Sleep(500 * time.Millisecond)

	tvm.vm.ctx.Lock.Lock()

	assert.False(txRequested, "tx should not have been requested")
	txGossipedLock.Lock()
	assert.Equal(0, txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()
	assert.True(tvm.vm.mempool.Has(tx.ID()))

	tvm.vm.ctx.Lock.Unlock()

	// show that tx is not re-gossiped
	assert.NoError(tvm.vm.AppGossip(context.Background(), nodeID, msgBytes))

	tvm.vm.ctx.Lock.Lock()

	txGossipedLock.Lock()
	assert.Equal(0, txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()

	// show that conflicting tx is not added to mempool
	msg = atomic.GossipAtomicTx{
		Tx: conflictingTx,
	}
	marshaller = atomic.GossipAtomicTxMarshaller{}
	txBytes, err = marshaller.MarshalGossip(&msg)
	assert.NoError(err)

	tvm.vm.ctx.Lock.Unlock()

	msgBytes, err = buildAtomicPushGossip(txBytes)
	assert.NoError(err)
	assert.NoError(tvm.vm.AppGossip(context.Background(), nodeID, msgBytes))

	tvm.vm.ctx.Lock.Lock()

	assert.False(txRequested, "tx should not have been requested")
	txGossipedLock.Lock()
	assert.Equal(0, txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()
	assert.False(tvm.vm.mempool.Has(conflictingTx.ID()), "conflicting tx should not be in the atomic mempool")
}

// show that txs already marked as invalid are not re-requested on gossiping
func TestMempoolAtmTxsAppGossipHandlingDiscardedTx(t *testing.T) {
	assert := assert.New(t)

	tvm := newVM(t, testVMConfig{})
	defer func() {
		assert.NoError(tvm.vm.Shutdown(context.Background()))
	}()
	mempool := tvm.vm.mempool

	var (
		txGossiped     int
		txGossipedLock sync.Mutex
		txRequested    bool
	)
	tvm.appSender.CantSendAppGossip = false
	tvm.appSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error {
		txGossipedLock.Lock()
		defer txGossipedLock.Unlock()

		txGossiped++
		return nil
	}
	tvm.appSender.SendAppRequestF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
		txRequested = true
		return nil
	}

	// Create a transaction and mark it as invalid by discarding it
	importTxs := createImportTxOptions(t, tvm.vm, tvm.atomicMemory)
	tx, conflictingTx := importTxs[0], importTxs[1]
	txID := tx.ID()

	mempool.AddRemoteTx(tx)
	mempool.NextTx()
	mempool.DiscardCurrentTx(txID)

	// Check the mempool does not contain the discarded transaction
	assert.False(mempool.Has(txID))

	// Gossip the transaction to the VM and ensure that it is not added to the mempool
	// and is not re-gossipped.
	nodeID := ids.GenerateTestNodeID()
	msg := atomic.GossipAtomicTx{
		Tx: tx,
	}
	marshaller := atomic.GossipAtomicTxMarshaller{}
	txBytes, err := marshaller.MarshalGossip(&msg)
	assert.NoError(err)

	tvm.vm.ctx.Lock.Unlock()

	msgBytes, err := buildAtomicPushGossip(txBytes)
	assert.NoError(err)
	assert.NoError(tvm.vm.AppGossip(context.Background(), nodeID, msgBytes))

	tvm.vm.ctx.Lock.Lock()

	assert.False(txRequested, "tx shouldn't be requested")
	txGossipedLock.Lock()
	assert.Zero(txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()

	assert.False(mempool.Has(txID))

	tvm.vm.ctx.Lock.Unlock()

	// Conflicting tx must be submitted over the API to be included in push gossip.
	// (i.e., txs received via p2p are not included in push gossip)
	// This test adds it directly to the mempool + gossiper to simulate that.
	tvm.vm.mempool.AddRemoteTx(conflictingTx)
	tvm.vm.atomicTxPushGossiper.Add(&atomic.GossipAtomicTx{Tx: conflictingTx})
	time.Sleep(500 * time.Millisecond)

	tvm.vm.ctx.Lock.Lock()

	assert.False(txRequested, "tx shouldn't be requested")
	txGossipedLock.Lock()
	assert.Equal(1, txGossiped, "conflicting tx should have been gossiped")
	txGossipedLock.Unlock()

	assert.False(mempool.Has(txID))
	assert.True(mempool.Has(conflictingTx.ID()))
}

func buildAtomicPushGossip(txBytes []byte) ([]byte, error) {
	inboundGossip := &sdk.PushGossip{
		Gossip: [][]byte{txBytes},
	}
	inboundGossipBytes, err := proto.Marshal(inboundGossip)
	if err != nil {
		return nil, err
	}

	inboundGossipMsg := append(binary.AppendUvarint(nil, p2p.AtomicTxGossipHandlerID), inboundGossipBytes...)
	return inboundGossipMsg, nil
}
