// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"

	"github.com/ava-labs/avalanchego/vms/components/chain"

	"github.com/stretchr/testify/assert"
)

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

			// show that build block include that tx and tx is still in mempool
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

			// try to add a conflicting tx again (don't use issueTx because will fail
			// verification)
			err = mempool.AddTx(conflictingTx)
			assert.ErrorIs(err, errConflictingAtomicTx)
			has = mempool.has(conflictingTxID)
			assert.False(has, "conflicting tx in mempool")
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

	// create candidate tx
	tx, _ := getValidImportTx(vm, sharedMemory, t)

	// shortcut to simulated almost filled mempool
	mempool.maxSize = 0

	err := mempool.AddTx(tx)
	assert.ErrorIs(err, errTooManyAtomicTx, "max mempool size breached")

	// shortcut to simulated empty mempool
	mempool.maxSize = defaultMempoolSize

	err = mempool.AddTx(tx)
	assert.NoError(err)
}
