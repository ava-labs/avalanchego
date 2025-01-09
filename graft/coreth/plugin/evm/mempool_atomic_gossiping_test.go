// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/prometheus/client_golang/prometheus"

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
				err := vm.Shutdown(context.Background())
				assert.NoError(err)
			}()
			mempool := vm.mempool

			// generate a valid and conflicting tx
			var (
				tx, conflictingTx *atomic.Tx
			)
			if name == "import" {
				importTxs := createImportTxOptions(t, vm, sharedMemory)
				tx, conflictingTx = importTxs[0], importTxs[1]
			} else {
				exportTxs := createExportTxOptions(t, vm, issuer, sharedMemory)
				tx, conflictingTx = exportTxs[0], exportTxs[1]
			}
			txID := tx.ID()
			conflictingTxID := conflictingTx.ID()

			// add a tx to the mempool
			err := vm.mempool.AddLocalTx(tx)
			assert.NoError(err)
			has := mempool.Has(txID)
			assert.True(has, "valid tx not recorded into mempool")

			// try to add a conflicting tx
			err = vm.mempool.AddLocalTx(conflictingTx)
			assert.ErrorIs(err, atomic.ErrConflictingAtomicTx)
			has = mempool.Has(conflictingTxID)
			assert.False(has, "conflicting tx in mempool")

			<-issuer

			has = mempool.Has(txID)
			assert.True(has, "valid tx not recorded into mempool")

			// Show that BuildBlock generates a block containing [txID] and that it is
			// still present in the mempool.
			blk, err := vm.BuildBlock(context.Background())
			assert.NoError(err, "could not build block out of mempool")

			evmBlk, ok := blk.(*chain.BlockWrapper).Block.(*Block)
			assert.True(ok, "unknown block type")

			assert.Equal(txID, evmBlk.atomicTxs[0].ID(), "block does not include expected transaction")

			has = mempool.Has(txID)
			assert.True(has, "tx should stay in mempool until block is accepted")

			err = blk.Verify(context.Background())
			assert.NoError(err)

			err = blk.Accept(context.Background())
			assert.NoError(err)

			has = mempool.Has(txID)
			assert.False(has, "tx shouldn't be in mempool after block is accepted")
		})
	}
}

// a valid tx shouldn't be added to the mempool if this would exceed the
// mempool's max size
func TestMempoolMaxMempoolSizeHandling(t *testing.T) {
	assert := assert.New(t)

	mempool, err := atomic.NewMempool(&snow.Context{}, prometheus.NewRegistry(), 1, nil)
	assert.NoError(err)
	// create candidate tx (we will drop before validation)
	tx := atomic.GenerateTestImportTx()

	assert.NoError(mempool.AddRemoteTx(tx))
	assert.True(mempool.Has(tx.ID()))
	// promote tx to be issued
	_, ok := mempool.NextTx()
	assert.True(ok)
	mempool.IssueCurrentTxs()

	// try to add one more tx
	tx2 := atomic.GenerateTestImportTx()
	assert.ErrorIs(mempool.AddRemoteTx(tx2), atomic.ErrTooManyAtomicTx)
	assert.False(mempool.Has(tx2.ID()))
}

// mempool will drop transaction with the lowest fee
func TestMempoolPriorityDrop(t *testing.T) {
	assert := assert.New(t)

	// we use AP3 genesis here to not trip any block fees
	importAmount := uint64(50000000)
	_, vm, _, _, _ := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase3, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
		testShortIDAddrs[1]: importAmount,
	})
	defer func() {
		err := vm.Shutdown(context.Background())
		assert.NoError(err)
	}()
	mempool, err := atomic.NewMempool(vm.ctx, prometheus.NewRegistry(), 1, vm.verifyTxAtTip)
	assert.NoError(err)

	tx1, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	if err != nil {
		t.Fatal(err)
	}
	assert.NoError(mempool.AddRemoteTx(tx1))
	assert.True(mempool.Has(tx1.ID()))

	tx2, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[1], initialBaseFee, []*secp256k1.PrivateKey{testKeys[1]})
	if err != nil {
		t.Fatal(err)
	}
	assert.ErrorIs(mempool.AddRemoteTx(tx2), atomic.ErrInsufficientAtomicTxFee)
	assert.True(mempool.Has(tx1.ID()))
	assert.False(mempool.Has(tx2.ID()))

	tx3, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[1], new(big.Int).Mul(initialBaseFee, big.NewInt(2)), []*secp256k1.PrivateKey{testKeys[1]})
	if err != nil {
		t.Fatal(err)
	}
	assert.NoError(mempool.AddRemoteTx(tx3))
	assert.False(mempool.Has(tx1.ID()))
	assert.False(mempool.Has(tx2.ID()))
	assert.True(mempool.Has(tx3.ID()))
}
