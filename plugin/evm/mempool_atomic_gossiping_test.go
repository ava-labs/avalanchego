// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/vms/components/chain"

	"github.com/stretchr/testify/assert"
)

// shows that a locally generated AtomicTx can be added to mempool and then
// removed by inclusion in a block
func TestMempoolAddLocallyCreateAtomicTx(t *testing.T) {
	assert := assert.New(t)

	issuer, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	// add a tx to it
	tx := getValidTx(vm, sharedMemory, t)
	txID := tx.ID()

	err := vm.issueTx(tx, true /*=local*/)
	assert.NoError(err)

	<-issuer

	has := mempool.has(txID)
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
}

// a valid tx shouldn't be added to the mempool if this would exceed the
// mempool's max size
func TestMempoolMaxMempoolSizeHandling(t *testing.T) {
	assert := assert.New(t)

	_, vm, _, sharedMemory, _ := GenesisVM(t, true, genesisJSONApricotPhase0, "", "")
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := vm.mempool

	// create candidate tx
	tx := getValidTx(vm, sharedMemory, t)

	// shortcut to simulated almost filled mempool
	mempool.maxSize = 0

	err := mempool.AddTx(tx)
	assert.Equal(errTooManyAtomicTx, err, "max mempool size breached")

	// shortcut to simulated empty mempool
	mempool.maxSize = defaultMempoolSize

	err = mempool.AddTx(tx)
	assert.NoError(err)
}
