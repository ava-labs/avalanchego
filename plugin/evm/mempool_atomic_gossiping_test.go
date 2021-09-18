// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/coreth/params"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"github.com/stretchr/testify/assert"
)

// forceAddTx forcibly adds a *Tx to the mempool and bypasses all verification.
func (m *Mempool) forceAddTx(tx *Tx) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	gasPrice, err := m.atomicTxGasPrice(tx)
	if err != nil {
		return err
	}
	m.txHeap.Push(tx, gasPrice)
	m.addPending()
	return nil
}

// shows that a locally generated AtomicTx can be added to mempool and then
// removed by inclusion in a block
func TestMempoolAddLocallyCreateAtomicTx(t *testing.T) {
	for _, name := range []string{"import", "export"} {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			// we use AP3 genesis here to not trip any block fees
			issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase3, "", "")
			defer func() {
				err := vm.Shutdown()
				assert.NoError(err)
			}()
			mempool := vm.mempool

			// generate a valid and conflicting tx
			var tx, conflictingTx *Tx
			if name == "import" {
				tx, conflictingTx = getValidImportTx(vm, sharedMemory, t)
			} else {
				tx, conflictingTx = getValidExportTx(vm, issuer, sharedMemory, t)
			}
			txID := tx.ID()
			conflictingTxID := conflictingTx.ID()

			// add a tx to the mempool
			err := vm.issueTx(tx, true /*=local*/)
			assert.NoError(err)
			has := mempool.has(txID)
			assert.True(has, "valid tx not recorded into mempool")

			// try to add a conflicting tx
			err = vm.issueTx(conflictingTx, true /*=local*/)
			assert.ErrorIs(err, errConflictingAtomicTx)
			has = mempool.has(conflictingTxID)
			assert.False(has, "conflicting tx in mempool")

			<-issuer

			has = mempool.has(txID)
			assert.True(has, "valid tx not recorded into mempool")

			// Show that BuildBlock generates a block containing [txID] and that it is
			// still present in the mempool.
			blk, err := vm.BuildBlock()
			assert.NoError(err, "could not build block out of mempool")

			evmBlk, ok := blk.(*chain.BlockWrapper).Block.(*Block)
			assert.True(ok, "unknown block type")

			retrievedTx, err := vm.extractAtomicTx(evmBlk.ethBlock)
			assert.NoError(err, "could not extract atomic tx")
			assert.Equal(txID, retrievedTx.ID(), "block does not include expected transaction")

			has = mempool.has(txID)
			assert.True(has, "tx should stay in mempool until block is accepted")

			err = blk.Verify()
			assert.NoError(err)

			err = blk.Accept()
			assert.NoError(err)

			has = mempool.has(txID)
			assert.False(has, "tx shouldn't be in mempool after block is accepted")
		})
	}
}

// a valid tx shouldn't be added to the mempool if this would exceed the
// mempool's max size
func TestMempoolMaxMempoolSizeHandling(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase4, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	mempool := vm.mempool

	// create candidate tx (we will drop before validation)
	tx, _ := getValidImportTx(vm, sharedMemory, t)

	// shortcut to simulated almost filled mempool
	mempool.maxSize = 0

	assert.ErrorIs(mempool.AddTx(tx), errTooManyAtomicTx)
	assert.False(mempool.has(tx.ID()))

	// shortcut to simulated empty mempool
	mempool.maxSize = defaultMempoolSize

	assert.NoError(mempool.AddTx(tx))
	assert.True(mempool.has(tx.ID()))
}

func createImportTx(t *testing.T, vm *VM, txID ids.ID, feeAmount uint64) *Tx {
	var importAmount uint64 = 10000000
	importTx := &UnsignedImportTx{
		NetworkID:    testNetworkID,
		BlockchainID: testCChainID,
		SourceChain:  testXChainID,
		ImportedInputs: []*avax.TransferableInput{
			{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(0),
				},
				Asset: avax.Asset{ID: testAvaxAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: importAmount,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
			{
				UTXOID: avax.UTXOID{
					TxID:        txID,
					OutputIndex: uint32(1),
				},
				Asset: avax.Asset{ID: testAvaxAssetID},
				In: &secp256k1fx.TransferInput{
					Amt: importAmount,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			},
		},
		Outs: []EVMOutput{
			{
				Address: testEthAddrs[0],
				Amount:  importAmount - feeAmount,
				AssetID: testAvaxAssetID,
			},
			{
				Address: testEthAddrs[1],
				Amount:  importAmount,
				AssetID: testAvaxAssetID,
			},
		},
	}

	// Sort the inputs and outputs to ensure the transaction is canonical
	avax.SortTransferableInputs(importTx.ImportedInputs)
	SortEVMOutputs(importTx.Outs)

	tx := &Tx{UnsignedAtomicTx: importTx}
	// Sign with the correct key
	if err := tx.Sign(vm.codec, [][]*crypto.PrivateKeySECP256K1R{{testKeys[0]}}); err != nil {
		t.Fatal(err)
	}

	return tx
}

// mempool will drop transaction with the lowest fee
func TestMempoolPriorityDrop(t *testing.T) {
	assert := assert.New(t)

	// we use AP3 genesis here to not trip any block fees
	_, vm, _, _, _ := GenesisVM(t, true, genesisJSONApricotPhase3, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	mempool := vm.mempool
	mempool.maxSize = 1

	tx1 := createImportTx(t, vm, ids.ID{1}, params.AvalancheAtomicTxFee)
	assert.NoError(mempool.AddTx(tx1))
	assert.True(mempool.has(tx1.ID()))
	tx2 := createImportTx(t, vm, ids.ID{2}, params.AvalancheAtomicTxFee)
	assert.ErrorIs(mempool.AddTx(tx2), errInsufficientAtomicTxFee)
	assert.True(mempool.has(tx1.ID()))
	assert.False(mempool.has(tx2.ID()))
	tx3 := createImportTx(t, vm, ids.ID{3}, 2*params.AvalancheAtomicTxFee)
	assert.NoError(mempool.AddTx(tx3))
	assert.False(mempool.has(tx1.ID()))
	assert.False(mempool.has(tx2.ID()))
	assert.True(mempool.has(tx3.ID()))
}
