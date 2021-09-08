// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// shows that a locally generated CreateChainTx can be added to mempool and then
// removed by inclusion in a block
func TestBlockBuilder_Add_LocallyCreate_CreateChainTx(t *testing.T) {
	assert := assert.New(t)

	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	mempool := &vm.blockBuilder

	// add a tx to it
	tx := getTheValidTx(vm, t)
	txID := tx.ID()

	err := mempool.IssueTx(tx)
	assert.NoError(err, "couldn't add tx to mempool")

	has := mempool.Has(txID)
	assert.True(has, "valid tx not recorded into mempool")

	// show that build block include that tx and removes it from mempool
	blkIntf, err := vm.BuildBlock()
	assert.NoError(err, "couldn't build block out of mempool")

	blk, ok := blkIntf.(*StandardBlock)
	assert.True(ok, "expected standard block")
	assert.Len(blk.Txs, 1, "standard block should include a single transaction")
	assert.Equal(txID, blk.Txs[0].ID(), "standard block does not include expected transaction")

	has = mempool.Has(txID)
	assert.False(has, "tx included in block is still recorded into mempool")
}

// shows that valid tx is not added to mempool if this would exceed its maximum
// size
func TestBlockBuilder_MaxMempoolSizeHandling(t *testing.T) {
	assert := assert.New(t)

	vm, _, _ := defaultVM()
	vm.ctx.Lock.Lock()
	defer func() {
		err := vm.Shutdown()
		assert.NoError(err)
		vm.ctx.Lock.Unlock()
	}()
	vm.gossipActivationTime = time.Unix(0, 0) // enable mempool gossiping
	blockBuilder := &vm.blockBuilder
	mempool := blockBuilder.Mempool.(*mempool)

	// create candidate tx
	tx := getTheValidTx(vm, t)

	// shortcut to simulated almost filled mempool
	mempool.totalBytesSize = maxMempoolSize - len(tx.Bytes()) + 1

	err := blockBuilder.AddUncheckedTx(tx)
	assert.Equal(errMempoolFull, err, "max mempool size breached")

	// shortcut to simulated almost filled mempool
	mempool.totalBytesSize = maxMempoolSize - len(tx.Bytes())

	err = blockBuilder.AddUncheckedTx(tx)
	assert.NoError(err, "should have added tx to mempool")
}
