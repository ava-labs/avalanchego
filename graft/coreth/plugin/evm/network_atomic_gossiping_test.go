// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

func getTheValidTx(vm *VM, sharedMemory *atomic.Memory, t *testing.T) *Tx {
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

	return importTx
}

func getTheIllFormedTx(vm *VM, sharedMemory *atomic.Memory, t *testing.T) *Tx {
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

	// code below extracted from newImportTx to make an invalidTx
	kc := secp256k1fx.NewKeychain()
	kc.Add(testKeys[0])

	atomicUTXOs, _, _, err := vm.GetAtomicUTXOs(vm.ctx.XChainID, kc.Addresses(),
		ids.ShortEmpty, ids.Empty, -1)
	if err != nil {
		t.Fatal(err)
	}

	importedInputs := []*avax.TransferableInput{}
	signers := [][]*crypto.PrivateKeySECP256K1R{}

	importedAmount := make(map[ids.ID]uint64)
	now := vm.clock.Unix()
	for _, utxo := range atomicUTXOs {
		inputIntf, utxoSigners, err := kc.Spend(utxo.Out, now)
		if err != nil {
			continue
		}
		input, ok := inputIntf.(avax.TransferableIn)
		if !ok {
			continue
		}
		aid := utxo.AssetID()
		importedAmount[aid], err = math.Add64(importedAmount[aid], input.Amount())
		if err != nil {
			t.Fatal(err)
		}
		importedInputs = append(importedInputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In:     input,
		})
		signers = append(signers, utxoSigners)
	}
	avax.SortTransferableInputsWithSigners(importedInputs, signers)
	importedAVAXAmount := importedAmount[vm.ctx.AVAXAssetID]
	outs := []EVMOutput{}

	txFeeWithoutChange := params.AvalancheAtomicTxFee
	txFeeWithChange := params.AvalancheAtomicTxFee

	// AVAX output
	if importedAVAXAmount < txFeeWithoutChange { // imported amount goes toward paying tx fee
		t.Fatal(errInsufficientFundsForFee)
	} else if importedAVAXAmount > txFeeWithChange {
		outs = append(outs, EVMOutput{
			Address: testEthAddrs[0],
			Amount:  importedAVAXAmount - txFeeWithChange,
			AssetID: vm.ctx.AVAXAssetID,
		})
	}

	// This will create unique outputs (in the context of sorting)
	// since each output will have a unique assetID
	for assetID, amount := range importedAmount {
		// Skip the AVAX amount since it has already been included
		// and skip any input with an amount of 0
		if assetID == vm.ctx.AVAXAssetID || amount == 0 {
			continue
		}
		outs = append(outs, EVMOutput{
			Address: testEthAddrs[0],
			Amount:  amount,
			AssetID: assetID,
		})
	}

	// If no outputs are produced, return an error.
	// Note: this can happen if there is exactly enough AVAX to pay the
	// transaction fee, but no other funds to be imported.
	if len(outs) == 0 {
		t.Fatal(errNoEVMOutputs)
	}

	SortEVMOutputs(outs)

	// Create the transaction
	utx := &UnsignedImportTx{
		NetworkID:      vm.ctx.NetworkID,
		BlockchainID:   vm.ctx.ChainID,
		Outs:           outs,
		ImportedInputs: importedInputs,
		SourceChain:    ids.ID{'f', 'a', 'k', 'e'}, // This should make the tx invalid
	}
	tx := &Tx{UnsignedAtomicTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		t.Fatal(err)
	}
	return tx
}

// shows that an atomic tx received as gossip response can be added to the
// mempool and then removed by inclusion in a block
func TestMempool_Add_Gossiped_AtomicTx(t *testing.T) {
	assert := assert.New(t)

	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool
	net := vm.network

	// create tx to be gossiped
	tx := getTheValidTx(vm, sharedMemory, t)
	txID := tx.ID()

	// gossip tx and check it is accepted
	nodeID := ids.GenerateTestShortID()
	msg := message.AtomicTx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	net.requestID++
	net.requestsAtmContent[net.requestID] = txID
	err = vm.AppResponse(nodeID, net.requestID, msgBytes)
	assert.NoError(err)

	<-issuer

	has := mempool.has(txID)
	assert.True(has, "issued tx should be recorded into mempool")

	// show that build block include that tx and tx is still in mempool
	blk, err := vm.BuildBlock()
	assert.NoError(err, "failed to build block from the mempool")

	evmBlk, ok := blk.(*chain.BlockWrapper).Block.(*Block)
	assert.True(ok, "unknown block type")

	retrievedTx, err := vm.extractAtomicTx(evmBlk.ethBlock)
	assert.NoError(err)
	assert.Equal(txID, retrievedTx.ID(), "block contains wrong transaction")

	has = mempool.has(txID)
	assert.True(has, "tx should stay in mempool till block is accepted")

	err = blk.Verify()
	assert.NoError(err)

	err = blk.Accept()
	assert.NoError(err)

	has = mempool.has(txID)
	assert.False(has, "tx should have been removed from the mempool after it was accepted")
}

// show that a tx discovered by a GossipResponse is re-gossiped after being
// added to the mempool
func TestMempool_AtmTxs_AppResponseHandling(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool
	net := vm.network

	var (
		wasGossiped   bool
		gossipedBytes []byte
	)
	sender.SendAppGossipF = func(b []byte) error {
		wasGossiped = true
		gossipedBytes = b
		return nil
	}

	// create tx to be received from AppGossipResponse
	tx := getTheValidTx(vm, sharedMemory, t)
	txID := tx.ID()

	// responses with unknown requestID are rejected
	nodeID := ids.GenerateTestShortID()
	msg := message.AtomicTx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	net.requestID++
	net.requestsAtmContent[net.requestID] = txID

	// Should drop an unexpected response
	unknownReqID := net.requestID + 1
	err = vm.AppResponse(nodeID, unknownReqID, msgBytes)
	assert.NoError(err)

	has := mempool.has(txID)
	assert.False(has, "responses with unknown requestID should not affect mempool")

	assert.False(wasGossiped, "responses with unknown requestID should not result in gossiping")

	// received tx and check it is accepted and re-gossiped
	err = vm.AppResponse(nodeID, net.requestID, msgBytes)
	assert.NoError(err)

	has = mempool.has(txID)
	assert.True(has, "valid tx not recorded into mempool")

	assert.True(wasGossiped, "valid tx should have been re-gossiped")

	// show that gossiped bytes can be duly decoded
	_, err = message.Parse(gossipedBytes)
	assert.NoError(err)

	// show that if tx is not accepted to mempool is not re-gossiped
	wasGossiped = false

	net.requestID++
	net.requestsAtmContent[net.requestID] = txID

	err = vm.AppResponse(nodeID, net.requestID, msgBytes)
	assert.NoError(err)

	assert.False(wasGossiped, "unaccepted tx should have not been re-gossiped")
}

// show that invalid txs are not accepted to mempool, nor rejected
func TestMempool_AtmTxs_AppResponseHandling_InvalidTx(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool
	net := vm.network

	var wasGossiped bool
	sender.SendAppGossipF = func([]byte) error {
		wasGossiped = true
		return nil
	}

	// create an invalid tx
	tx := getTheIllFormedTx(vm, sharedMemory, t)
	txID := tx.ID()

	// gossip tx and check it is accepted and re-gossiped
	nodeID := ids.GenerateTestShortID()
	msg := message.AtomicTx{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	net.requestID++
	net.requestsAtmContent[net.requestID] = txID
	err = vm.AppResponse(nodeID, net.requestID, msgBytes)
	assert.NoError(err)

	has := mempool.has(txID)
	assert.False(has, "invalid tx should not be issued to mempool")

	rejected := mempool.isRejected(txID)
	assert.True(rejected, "invalid tx should be marked as rejected")

	assert.False(wasGossiped, "invalid tx should not be re-gossiped")
}

// show that a txID discovered from gossip is requested to the same node only if
// the txID is unknown
func TestMempool_AtmTxs_AppGossipHandling(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	nodeID := ids.GenerateTestShortID()

	var (
		txRequested       bool
		txRequestedByNode bool
		requestedBytes    []byte
	)
	sender.SendAppRequestF = func(nodes ids.ShortSet, reqID uint32, resp []byte) error {
		txRequested = true
		if nodes.Contains(nodeID) {
			txRequestedByNode = true
		}
		requestedBytes = resp
		return nil
	}

	// create a tx
	tx := getTheValidTx(vm, sharedMemory, t)
	txID := tx.ID()

	// gossip tx and check it is accepted and re-gossiped
	msg := message.AtomicTxNotify{
		TxID: txID,
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	// show that unknown txID is requested
	err = vm.AppGossip(nodeID, msgBytes)
	assert.NoError(err)
	assert.True(txRequested, "tx should have been requested")
	assert.True(txRequestedByNode, "tx should have been by the gossiper node")

	requestMsgIntf, err := message.Parse(requestedBytes)
	assert.NoError(err)

	requestMsg, ok := requestMsgIntf.(*message.AtomicTxNotify)
	assert.True(ok)
	assert.Equal(txID, requestMsg.TxID)

	// show that known txID is not requested
	err = mempool.AddTx(tx)
	assert.NoError(err)

	txRequested = false
	err = vm.AppGossip(nodeID, msgBytes)
	assert.NoError(err)
	assert.False(txRequested, "known txID should not be requested")
}

// show that txs already marked as invalid are not re-requested on gossiping
func TestMempool_AtmTxs_AppGossipHandling_InvalidTx(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	var txRequested bool
	sender.SendAppRequestF = func(ids.ShortSet, uint32, []byte) error {
		txRequested = true
		return nil
	}

	// create a tx and mark as invalid
	tx := getTheValidTx(vm, sharedMemory, t)
	txID := tx.ID()

	mempool.AddTx(tx)
	mempool.NextTx()
	mempool.DiscardCurrentTx()

	rejected := mempool.isRejected(txID)
	assert.True(rejected)

	// gossip tx and check it is accepted and re-gossiped
	nodeID := ids.GenerateTestShortID()
	msg := message.AtomicTxNotify{
		TxID: txID,
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	err = vm.AppGossip(nodeID, msgBytes)
	assert.NoError(err)
	assert.False(txRequested, "rejected tx shouldn't be requested")
}

// show that a node answers to a request with a response if it has the requested
// tx
func TestMempool_AtmTxs_AppRequestHandling(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	var (
		responded      bool
		respondedBytes []byte
	)
	sender.SendAppResponseF = func(nodeID ids.ShortID, reqID uint32, resp []byte) error {
		responded = true
		respondedBytes = resp
		return nil
	}

	// create a tx
	tx := getTheValidTx(vm, sharedMemory, t)
	txID := tx.ID()

	// show that there is no response if tx is unknown
	nodeID := ids.GenerateTestShortID()
	msg := message.AtomicTxNotify{
		TxID: txID,
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	err = vm.AppRequest(nodeID, 0, msgBytes)
	assert.NoError(err)
	assert.False(responded, "there should be no response with unknown tx")

	// show that there is response if tx is known
	err = mempool.AddTx(tx)
	assert.NoError(err, "couldn't add tx to mempool")

	err = vm.AppRequest(nodeID, 0, msgBytes)
	assert.NoError(err)
	assert.True(responded, "there should be a response with known tx")

	replyIntf, err := message.Parse(respondedBytes)
	assert.NoError(err)

	reply, ok := replyIntf.(*message.AtomicTx)
	assert.True(ok)
	assert.Equal(tx.Bytes(), reply.Tx)
}

// locally issued txs should be gossiped
func TestMempool_AtmTxs_IssueTxAndGossiping(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping

	var gossiped bool
	sender.SendAppGossipF = func([]byte) error {
		gossiped = true
		return nil
	}

	// add a tx to it
	tx := getTheValidTx(vm, sharedMemory, t)
	err := vm.issueTx(tx, true /*=local*/)
	assert.NoError(err)
	assert.True(gossiped, "expected call to SendAppGossip not issued")
}
