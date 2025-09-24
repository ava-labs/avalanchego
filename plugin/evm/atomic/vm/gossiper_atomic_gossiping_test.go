// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

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
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/vmtest"

	commonEng "github.com/ava-labs/avalanchego/snow/engine/common"
)

// show that a txID discovered from gossip is requested to the same node only if
// the txID is unknown
func TestMempoolAtmTxsAppGossipHandling(t *testing.T) {
	require := require.New(t)

	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{})
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	nodeID := ids.GenerateTestNodeID()

	var (
		txGossiped     int
		txGossipedLock sync.Mutex
		txRequested    bool
	)
	tvm.AppSender.CantSendAppGossip = false
	tvm.AppSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error {
		txGossipedLock.Lock()
		defer txGossipedLock.Unlock()

		txGossiped++
		return nil
	}
	tvm.AppSender.SendAppRequestF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
		txRequested = true
		return nil
	}

	// Create conflicting transactions
	importTxs := createImportTxOptions(t, vm, tvm.AtomicMemory)
	tx, conflictingTx := importTxs[0], importTxs[1]

	// gossip tx and check it is accepted and gossiped
	marshaller := atomic.TxMarshaller{}
	txBytes, err := marshaller.MarshalGossip(tx)
	require.NoError(err)
	vm.Ctx.Lock.Unlock()

	msgBytes, err := buildAtomicPushGossip(txBytes)
	require.NoError(err)

	// show that no txID is requested
	require.NoError(vm.AppGossip(context.Background(), nodeID, msgBytes))
	time.Sleep(500 * time.Millisecond)

	vm.Ctx.Lock.Lock()

	require.False(txRequested, "tx should not have been requested")
	txGossipedLock.Lock()
	require.Zero(txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()
	require.True(vm.AtomicMempool.Has(tx.ID()))

	vm.Ctx.Lock.Unlock()

	// show that tx is not re-gossiped
	require.NoError(vm.AppGossip(context.Background(), nodeID, msgBytes))

	vm.Ctx.Lock.Lock()

	txGossipedLock.Lock()
	require.Zero(txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()

	// show that conflicting tx is not added to mempool
	marshaller = atomic.TxMarshaller{}
	txBytes, err = marshaller.MarshalGossip(conflictingTx)
	require.NoError(err)

	vm.Ctx.Lock.Unlock()

	msgBytes, err = buildAtomicPushGossip(txBytes)
	require.NoError(err)
	require.NoError(vm.AppGossip(context.Background(), nodeID, msgBytes))

	vm.Ctx.Lock.Lock()

	require.False(txRequested, "tx should not have been requested")
	txGossipedLock.Lock()
	require.Zero(txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()
	require.False(vm.AtomicMempool.Has(conflictingTx.ID()), "conflicting tx should not be in the atomic mempool")
}

// show that txs already marked as invalid are not re-requested on gossiping
func TestMempoolAtmTxsAppGossipHandlingDiscardedTx(t *testing.T) {
	require := require.New(t)

	vm := newAtomicTestVM()
	tvm := vmtest.SetupTestVM(t, vm, vmtest.TestVMConfig{})
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	var (
		txGossiped     int
		txGossipedLock sync.Mutex
		txRequested    bool
	)
	tvm.AppSender.CantSendAppGossip = false
	tvm.AppSender.SendAppGossipF = func(context.Context, commonEng.SendConfig, []byte) error {
		txGossipedLock.Lock()
		defer txGossipedLock.Unlock()

		txGossiped++
		return nil
	}
	tvm.AppSender.SendAppRequestF = func(context.Context, set.Set[ids.NodeID], uint32, []byte) error {
		txRequested = true
		return nil
	}

	// Create a transaction and mark it as invalid by discarding it
	importTxs := createImportTxOptions(t, vm, tvm.AtomicMemory)
	tx, conflictingTx := importTxs[0], importTxs[1]
	txID := tx.ID()

	require.NoError(vm.AtomicMempool.AddRemoteTx(tx))
	vm.AtomicMempool.NextTx()
	vm.AtomicMempool.DiscardCurrentTx(txID)

	// Check the mempool does not contain the discarded transaction
	require.False(vm.AtomicMempool.Has(txID))

	// Gossip the transaction to the VM and ensure that it is not added to the mempool
	// and is not re-gossipped.
	nodeID := ids.GenerateTestNodeID()
	marshaller := atomic.TxMarshaller{}
	txBytes, err := marshaller.MarshalGossip(tx)
	require.NoError(err)

	vm.Ctx.Lock.Unlock()

	msgBytes, err := buildAtomicPushGossip(txBytes)
	require.NoError(err)
	require.NoError(vm.AppGossip(context.Background(), nodeID, msgBytes))

	vm.Ctx.Lock.Lock()

	require.False(txRequested, "tx shouldn't be requested")
	txGossipedLock.Lock()
	require.Zero(txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()

	require.False(vm.AtomicMempool.Has(txID))

	vm.Ctx.Lock.Unlock()

	// Conflicting tx must be submitted over the API to be included in push gossip.
	// (i.e., txs received via p2p are not included in push gossip)
	// This test adds it directly to the mempool + gossiper to simulate that.
	require.NoError(vm.AtomicMempool.AddRemoteTx(conflictingTx))
	vm.AtomicTxPushGossiper.Add(conflictingTx)
	time.Sleep(500 * time.Millisecond)

	vm.Ctx.Lock.Lock()

	require.False(txRequested, "tx shouldn't be requested")
	txGossipedLock.Lock()
	require.Equal(1, txGossiped, "conflicting tx should have been gossiped")
	txGossipedLock.Unlock()

	require.False(vm.AtomicMempool.Has(txID))
	require.True(vm.AtomicMempool.Has(conflictingTx.ID()))
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
