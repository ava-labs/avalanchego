// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/extstate"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/atomic/atomictest"
	"github.com/ava-labs/coreth/plugin/evm/extension"
	"github.com/ava-labs/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/coreth/predicate"
)

func TestAtomicSyncerVM(t *testing.T) {
	importAmount := 2000000 * units.Avax // 2M avax
	for _, test := range vmtest.SyncerVMTests {
		includedAtomicTxs := make([]*atomic.Tx, 0)

		t.Run(test.Name, func(t *testing.T) {
			genFn := func(i int, vm extension.InnerVM, gen *core.BlockGen) {
				atomicVM, ok := vm.(*VM)
				require.True(t, ok)
				b, err := predicate.NewResults().Bytes()
				require.NoError(t, err)
				gen.AppendExtra(b)
				switch i {
				case 0:
					// spend the UTXOs from shared memory
					importTx, err := atomicVM.newImportTx(atomicVM.Ctx.XChainID, vmtest.TestEthAddrs[0], vmtest.InitialBaseFee, vmtest.TestKeys[0:1])
					require.NoError(t, err)
					require.NoError(t, atomicVM.AtomicMempool.AddLocalTx(importTx))
					includedAtomicTxs = append(includedAtomicTxs, importTx)
				case 1:
					// export some of the imported UTXOs to test exportTx is properly synced
					state, err := vm.Ethereum().BlockChain().State()
					require.NoError(t, err)
					wrappedStateDB := extstate.New(state)
					exportTx, err := atomic.NewExportTx(
						atomicVM.Ctx,
						atomicVM.CurrentRules(),
						wrappedStateDB,
						atomicVM.Ctx.AVAXAssetID,
						importAmount/2,
						atomicVM.Ctx.XChainID,
						vmtest.TestShortIDAddrs[0],
						vmtest.InitialBaseFee,
						vmtest.TestKeys[0:1],
					)
					require.NoError(t, err)
					require.NoError(t, atomicVM.AtomicMempool.AddLocalTx(exportTx))
					includedAtomicTxs = append(includedAtomicTxs, exportTx)
				default: // Generate simple transfer transactions.
					pk := vmtest.TestKeys[0].ToECDSA()
					tx := types.NewTransaction(gen.TxNonce(vmtest.TestEthAddrs[0]), vmtest.TestEthAddrs[1], common.Big1, params.TxGas, vmtest.InitialBaseFee, nil)
					signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.Ethereum().BlockChain().Config().ChainID), pk)
					require.NoError(t, err)
					gen.AddTx(signedTx)
				}
			}
			newVMFn := func() (extension.InnerVM, dummy.ConsensusCallbacks) {
				vm := newAtomicTestVM()
				return vm, vm.createConsensusCallbacks()
			}

			afterInit := func(t *testing.T, params vmtest.SyncTestParams, vmSetup vmtest.SyncVMSetup, isServer bool) {
				atomicVM, ok := vmSetup.VM.(*VM)
				require.True(t, ok)

				alloc := map[ids.ShortID]uint64{
					vmtest.TestShortIDAddrs[0]: importAmount,
				}

				for addr, avaxAmount := range alloc {
					txID, err := ids.ToID(hashing.ComputeHash256(addr.Bytes()))
					require.NoError(t, err, "Failed to generate txID from addr")
					_, err = addUTXO(vmSetup.AtomicMemory, vmSetup.SnowCtx, txID, 0, vmSetup.SnowCtx.AVAXAssetID, avaxAmount, addr)
					require.NoError(t, err, "Failed to add UTXO to shared memory")
				}
				if isServer {
					serverAtomicTrie := atomicVM.AtomicBackend.AtomicTrie()
					// Calling AcceptTrie with SyncableInterval creates a commit for the atomic trie
					committed, err := serverAtomicTrie.AcceptTrie(params.SyncableInterval, serverAtomicTrie.LastAcceptedRoot())
					require.NoError(t, err)
					require.True(t, committed)
					require.NoError(t, atomicVM.VersionDB().Commit())
				}
			}

			testSetup := &vmtest.SyncTestSetup{
				NewVM:     newVMFn,
				GenFn:     genFn,
				AfterInit: afterInit,
				ExtraSyncerVMTest: func(t *testing.T, syncerVMSetup vmtest.SyncVMSetup) {
					// check atomic memory was synced properly
					syncerVM := syncerVMSetup.VM
					atomicVM, ok := syncerVM.(*VM)
					require.True(t, ok)
					syncerSharedMemories := atomictest.NewSharedMemories(syncerVMSetup.AtomicMemory, atomicVM.Ctx.ChainID, atomicVM.Ctx.XChainID)

					for _, tx := range includedAtomicTxs {
						atomicOps, err := atomictest.ConvertToAtomicOps(tx)
						require.NoError(t, err)
						syncerSharedMemories.AssertOpsApplied(t, atomicOps)
					}
				},
			}
			test.TestFunc(t, testSetup)
		})
	}
}
