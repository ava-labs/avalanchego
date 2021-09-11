// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/message"
)

func getValidTx(vm *VM, sharedMemory *atomic.Memory, t *testing.T) *Tx {
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

func getInvalidTx(vm *VM, sharedMemory *atomic.Memory, t *testing.T) *Tx {
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

// locally issued txs should be gossiped
func TestMempoolAtmTxsIssueTxAndGossiping(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, sender := GenesisVM(t, true, genesisJSONApricotPhase4, "", "")
	defer func() {
		assert.NoError(vm.Shutdown())
	}()

	// Create a simple tx
	tx := getValidTx(vm, sharedMemory, t)

	var gossiped int
	sender.CantSendAppGossip = false
	sender.SendAppGossipF = func(gossipedBytes []byte) error {
		notifyMsgIntf, err := message.Parse(gossipedBytes)
		assert.NoError(err)

		requestMsg, ok := notifyMsgIntf.(*message.AtomicTxNotify)
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
	assert.Equal(1, gossiped)

	// Test hash on retry
	assert.NoError(vm.GossipAtomicTx(tx))
	assert.Equal(1, gossiped)
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
		txGossiped  int
		txRequested bool
	)
	sender.CantSendAppGossip = false
	sender.SendAppGossipF = func(_ []byte) error {
		txGossiped++
		return nil
	}
	sender.SendAppRequestF = func(_ ids.ShortSet, _ uint32, _ []byte) error {
		txRequested = true
		return nil
	}

	// create a tx
	tx := getValidTx(vm, sharedMemory, t)

	// gossip tx and check it is accepted and gossiped
	msg := message.AtomicTxNotify{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	// show that unknown txID is requested
	assert.NoError(vm.AppGossip(nodeID, msgBytes))
	assert.False(txRequested, "tx should not have been requested")
	assert.Equal(1, txGossiped, "tx should have been gossiped")

	assert.NoError(vm.AppGossip(nodeID, msgBytes))
	assert.Equal(1, txGossiped, "tx should have only been gossiped once")
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
		txGossiped  int
		txRequested bool
	)
	sender.CantSendAppGossip = false
	sender.SendAppGossipF = func(_ []byte) error {
		txGossiped++
		return nil
	}
	sender.SendAppRequestF = func(ids.ShortSet, uint32, []byte) error {
		txRequested = true
		return nil
	}

	// create a tx and mark as invalid
	tx := getInvalidTx(vm, sharedMemory, t)
	txID := tx.ID()

	// TODO: AddTx should reject a transaction like this
	mempool.AddTx(tx)
	mempool.NextTx()
	mempool.DiscardCurrentTx()

	has := mempool.has(txID)
	assert.False(has)

	// gossip tx and check it is accepted and re-gossiped
	nodeID := ids.GenerateTestShortID()
	msg := message.AtomicTxNotify{
		Tx: tx.Bytes(),
	}
	msgBytes, err := message.Build(&msg)
	assert.NoError(err)

	assert.NoError(vm.AppGossip(nodeID, msgBytes))
	assert.False(txRequested, "tx shouldn't be requested")
	assert.Zero(txGossiped, "tx should not have been gossiped")
}
