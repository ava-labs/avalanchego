// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"testing"

	"github.com/ava-labs/coreth/plugin/evm/atomic"
	atomictxpool "github.com/ava-labs/coreth/plugin/evm/atomic/txpool"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shows that a locally generated AtomicTx can be added to mempool and then
// removed by inclusion in a block
func TestMempoolAddLocallyCreateAtomicTx(t *testing.T) {
	for _, name := range []string{"import", "export"} {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			// we use AP3 here to not trip any block fees
			fork := upgradetest.ApricotPhase3
			tvm := newVM(t, testVMConfig{
				fork: &fork,
			})
			defer func() {
				err := tvm.vm.Shutdown(context.Background())
				assert.NoError(err)
			}()
			mempool := tvm.atomicVM.AtomicMempool

			// generate a valid and conflicting tx
			var (
				tx, conflictingTx *atomic.Tx
			)
			if name == "import" {
				importTxs := createImportTxOptions(t, tvm.atomicVM, tvm.atomicMemory)
				tx, conflictingTx = importTxs[0], importTxs[1]
			} else {
				exportTxs := createExportTxOptions(t, tvm.atomicVM, tvm.atomicMemory)
				tx, conflictingTx = exportTxs[0], exportTxs[1]
			}
			txID := tx.ID()
			conflictingTxID := conflictingTx.ID()

			// add a tx to the mempool
			err := tvm.atomicVM.AtomicMempool.AddLocalTx(tx)
			assert.NoError(err)
			has := mempool.Has(txID)
			assert.True(has, "valid tx not recorded into mempool")

			// try to add a conflicting tx
			err = tvm.atomicVM.AtomicMempool.AddLocalTx(conflictingTx)
			assert.ErrorIs(err, atomictxpool.ErrConflict)
			has = mempool.Has(conflictingTxID)
			assert.False(has, "conflicting tx in mempool")

			require.Equal(t, common.PendingTxs, tvm.WaitForEvent(context.Background()))

			has = mempool.Has(txID)
			assert.True(has, "valid tx not recorded into mempool")

			// Show that BuildBlock generates a block containing [txID] and that it is
			// still present in the mempool.
			blk, err := tvm.vm.BuildBlock(context.Background())
			assert.NoError(err, "could not build block out of mempool")

			wrappedBlock, ok := blk.(*chain.BlockWrapper).Block.(extension.ExtendedBlock)
			assert.True(ok, "unknown block type")

			blockExtension, ok := wrappedBlock.GetBlockExtension().(atomic.AtomicBlockContext)
			assert.True(ok, "unknown block extension type")

			assert.Equal(txID, blockExtension.AtomicTxs()[0].ID(), "block does not include expected transaction")

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
