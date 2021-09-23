// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/coreth/plugin/evm/message"
)

// getValidImportTx returns 2 transactions that conflict with each other (both valid)
func getValidImportTx(vm *VM, sharedMemory *atomic.Memory, t *testing.T) (*Tx, *Tx) {
	importAmount := uint64(50000000)
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	importTx2, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[1], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	return importTx, importTx2
}

// getValidExportTx returns 2 transactions that conflict with each other (both valid)
func getValidExportTx(vm *VM, issuer chan common.Message, sharedMemory *atomic.Memory, t *testing.T) (*Tx, *Tx) {
	importAmount := uint64(50000000)
	utxoID := avax.UTXOID{
		TxID: ids.ID{
			0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
			0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
			0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
			0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
		},
	}

	utxo := &avax.UTXO{
		UTXOID: utxoID,
		Asset:  avax.Asset{ID: vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: importAmount,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{testKeys[0].PublicKey().Address()},
			},
		},
	}
	utxoBytes, err := vm.codec.Marshal(codecVersion, utxo)
	if err != nil {
		t.Fatal(err)
	}

	xChainSharedMemory := sharedMemory.NewSharedMemory(vm.ctx.XChainID)
	inputID := utxo.InputID()
	if err := xChainSharedMemory.Apply(map[ids.ID]*atomic.Requests{vm.ctx.ChainID: {PutRequests: []*atomic.Element{{
		Key:   inputID[:],
		Value: utxoBytes,
		Traits: [][]byte{
			testKeys[0].PublicKey().Address().Bytes(),
		},
	}}}}); err != nil {
		t.Fatal(err)
	}

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	if err := vm.issueTx(importTx, true /*=local*/); err != nil {
		t.Fatal(err)
	}

	<-issuer

	blk, err := vm.BuildBlock()
	if err != nil {
		t.Fatal(err)
	}

	if err := blk.Verify(); err != nil {
		t.Fatal(err)
	}

	if err := vm.SetPreference(blk.ID()); err != nil {
		t.Fatal(err)
	}

	if err := blk.Accept(); err != nil {
		t.Fatal(err)
	}

	exportAmount := uint64(5000000)

	testKeys1Addr := GetEthAddress(testKeys[0])
	exportId1, err := ids.ToShortID(testKeys1Addr[:])
	if err != nil {
		t.Fatal(err)
	}
	testKeys2Addr := GetEthAddress(testKeys[1])
	exportId2, err := ids.ToShortID(testKeys2Addr[:])
	if err != nil {
		t.Fatal(err)
	}

	exportTx1, err := vm.newExportTx(vm.ctx.AVAXAssetID, exportAmount, vm.ctx.XChainID, exportId1, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	exportTx2, err := vm.newExportTx(vm.ctx.AVAXAssetID, exportAmount, vm.ctx.XChainID, exportId2, initialBaseFee, []*crypto.PrivateKeySECP256K1R{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}

	return exportTx1, exportTx2
}

// locally issued txs should be gossiped
func TestMempoolAtmTxsIssueTxAndGossiping(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase4, "", "")
	defer func() {
		assert.NoError(vm.Shutdown())
	}()

	// Create a simple tx
	tx, conflictingTx := getValidImportTx(vm, sharedMemory, t)

	var gossiped int
	var gossipedLock sync.Mutex // needed to prevent race
	sender.CantSendAppGossip = false
	sender.SendAppGossipF = func(gossipedBytes []byte) error {
		gossipedLock.Lock()
		defer gossipedLock.Unlock()

		notifyMsgIntf, err := message.Parse(gossipedBytes)
		assert.NoError(err)

		requestMsg, ok := notifyMsgIntf.(*message.AtomicTx)
		assert.NotEmpty(requestMsg.Tx)
		assert.True(ok)

		txg := Tx{}
		_, err = Codec.Unmarshal(requestMsg.Tx, &txg)
		assert.NoError(err)
		unsignedBytes, err := Codec.Marshal(codecVersion, &txg.UnsignedAtomicTx)
		assert.NoError(err)
		txg.Initialize(unsignedBytes, requestMsg.Tx)
		assert.Equal(tx.ID(), txg.ID())
		gossiped++
		return nil
	}

	// Optimistically gossip raw tx
	assert.NoError(vm.issueTx(tx, true /*=local*/))
	time.Sleep(waitBlockTime * 3)
	gossipedLock.Lock()
	assert.Equal(1, gossiped)
	gossipedLock.Unlock()

	// Test hash on retry
	assert.NoError(vm.network.GossipAtomicTxs([]*Tx{tx}))
	gossipedLock.Lock()
	assert.Equal(1, gossiped)
	gossipedLock.Unlock()

	// Attempt to gossip conflicting tx
	assert.ErrorIs(vm.issueTx(conflictingTx, true /*=local*/), errConflictingAtomicTx)
	gossipedLock.Lock()
	assert.Equal(1, gossiped)
	gossipedLock.Unlock()
}

// show that a txID discovered from gossip is requested to the same node only if
// the txID is unknown
func TestMempoolAtmTxsAppGossipHandling(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase4, "", "")
	defer func() {
		assert.NoError(vm.Shutdown())
	}()

	nodeID := ids.GenerateTestShortID()

	var (
		txGossiped     int
		txGossipedLock sync.Mutex
		txRequested    bool
	)
	sender.CantSendAppGossip = false
	sender.SendAppGossipF = func(_ []byte) error {
		txGossipedLock.Lock()
		defer txGossipedLock.Unlock()

		txGossiped++
		return nil
	}
	sender.SendAppRequestF = func(_ ids.ShortSet, _ uint32, _ []byte) error {
		txRequested = true
		return nil
	}

	// create a tx
	tx, conflictingTx := getValidImportTx(vm, sharedMemory, t)

	// gossip tx and check it is accepted and gossiped
	msg := message.AtomicTx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	// show that no txID is requested
	assert.NoError(vm.AppGossip(nodeID, msgBytes))
	time.Sleep(waitBlockTime * 3)

	assert.False(txRequested, "tx should not have been requested")
	txGossipedLock.Lock()
	assert.Equal(1, txGossiped, "tx should have been gossiped")
	txGossipedLock.Unlock()
	assert.True(vm.mempool.has(tx.ID()))

	// show that tx is not re-gossiped
	assert.NoError(vm.AppGossip(nodeID, msgBytes))
	txGossipedLock.Lock()
	assert.Equal(1, txGossiped, "tx should have only been gossiped once")
	txGossipedLock.Unlock()

	// show that conflicting tx is not added to mempool
	msg = message.AtomicTx{
		Tx: conflictingTx.Bytes(),
	}
	msgBytes, err = message.Build(&msg)
	assert.NoError(err)
	assert.NoError(vm.AppGossip(nodeID, msgBytes))
	assert.False(txRequested, "tx should not have been requested")
	txGossipedLock.Lock()
	assert.Equal(1, txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()
	assert.False(vm.mempool.has(conflictingTx.ID()), "conflicting tx should not be in the atomic mempool")
}

// show that txs already marked as invalid are not re-requested on gossiping
func TestMempoolAtmTxsAppGossipHandlingDiscardedTx(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase4, "", "")
	defer func() {
		assert.NoError(vm.Shutdown())
	}()
	mempool := vm.mempool

	var (
		txGossiped     int
		txGossipedLock sync.Mutex
		txRequested    bool
	)
	sender.CantSendAppGossip = false
	sender.SendAppGossipF = func(_ []byte) error {
		txGossipedLock.Lock()
		defer txGossipedLock.Unlock()

		txGossiped++
		return nil
	}
	sender.SendAppRequestF = func(ids.ShortSet, uint32, []byte) error {
		txRequested = true
		return nil
	}

	// create a tx and mark as invalid
	tx, conflictingTx := getValidImportTx(vm, sharedMemory, t)
	txID := tx.ID()

	mempool.AddTx(tx)
	mempool.NextTx()
	mempool.DiscardCurrentTx()

	has := mempool.has(txID)
	assert.False(has)

	// gossip tx and check it isn't accepted or gossiped
	nodeID := ids.GenerateTestShortID()
	msg := message.AtomicTx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	assert.NoError(vm.AppGossip(nodeID, msgBytes))
	assert.False(txRequested, "tx shouldn't be requested")
	txGossipedLock.Lock()
	assert.Zero(txGossiped, "tx should not have been gossiped")
	txGossipedLock.Unlock()

	// gossip conflicting tx and ensure it is accepted and gossiped
	nodeID = ids.GenerateTestShortID()
	msg = message.AtomicTx{
		Tx: conflictingTx.Bytes(),
	}
	msgBytes, err = message.Build(&msg)
	assert.NoError(err)

	assert.NoError(vm.AppGossip(nodeID, msgBytes))
	time.Sleep(waitBlockTime * 3)
	assert.False(txRequested, "tx shouldn't be requested")
	txGossipedLock.Lock()
	assert.Equal(1, txGossiped, "conflicting tx should have been gossiped")
	txGossipedLock.Unlock()
}
