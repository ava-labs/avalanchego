// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"fmt"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/trie"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

func TestRecoverFromDatabase(t *testing.T) {
	t.Parallel()

	sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

	var srcDB database.Database
	srcHDB := saetest.NewHeightIndexDB()
	const commitInterval = 16
	ctx, src := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
		srcDB = c.db
		c.logLevel = logging.Warn
	}))
	srcCtx := ctx

	rng := rand.New(rand.NewPCG(0, 0)) //#nosec G404 -- Reproducibility is useful for tests

	for final := false; !final; {
		// We need to test rebuilding from trie roots reflecting (a) the last
		// synchronous block; (b) some committed state root; and (c) a few
		// blocks before/after the thresholds. Everything in between is merely
		// to advance the block number so is treated as a "quick" loop
		// iteration.
		last := src.lastAcceptedBlock(t)
		height := last.Height()
		quick := height < commitInterval && src.rawVM.last.settled.Load().Height() > 1
		final = height > commitInterval

		if !quick {
			src.sendTxsAndWaitUntilPending(t, src.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
				To:       nil,                      // execute `Data` as code for contract "construction"
				Data:     []byte{byte(vm.INVALID)}, // revert and consume all gas
				Gas:      params.TxGas + params.CreateGas + params.TxDataNonZeroGasFrontier + rng.Uint64N(2e6),
				GasPrice: big.NewInt(100),
			}))
		}

		vmTime.advance(850 * time.Millisecond)
		b := src.runConsensusLoop(t)
		if !quick {
			require.Len(t, b.Transactions(), 1, "transactions in block")
		}
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)

		if quick {
			continue
		}
		t.Run("recover", func(t *testing.T) {
			newDB := copyDB(t, srcDB)

			sutCtx, sut := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB.Clone()), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
				c.db = newDB
				c.logLevel = logging.Warn
			}))

			requireConsensusCriticalBlocks(t, src, sut)

			if !final {
				return
			}
			t.Run("build_on_recovered_VM", func(t *testing.T) {
				srcLast := src.lastAcceptedBlock(t)
				sutLast := sut.lastAcceptedBlock(t)
				if diff := cmp.Diff(srcLast, sutLast, blocks.CmpOpt()); diff != "" {
					t.Fatal(diff)
				}
				srcSDB := src.stateAt(t, srcLast.PostExecutionStateRoot())
				sutSDB := sut.stateAt(t, sutLast.PostExecutionStateRoot())
				if diff := cmp.Diff(srcSDB, sutSDB, cmputils.StateDBs()); diff != "" {
					t.Fatal(diff)
				}

				tx := src.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
					To:       &common.Address{},
					Gas:      params.TxGas,
					GasPrice: big.NewInt(100),
				})

				for _, sys := range []struct {
					name string
					ctx  context.Context //nolint:containedctx // Ephemeral so not in contravention of https://go.dev/blog/context-and-structs
					*SUT
				}{
					{"source", srcCtx, src},
					{"recovered", sutCtx, sut},
				} {
					t.Run(sys.name, func(t *testing.T) {
						b := sys.runConsensusLoop(t, tx)
						require.Len(t, b.Transactions(), 1)
						require.NoError(t, b.WaitUntilExecuted(sys.ctx))
					})
				}
			})
		})
	}
}

func TestRecoverSimple(t *testing.T) {
	t.Parallel()

	const commitInterval = 16
	tests := []struct {
		name      string
		numBlocks int
		archival  bool
	}{
		{
			name:      "archival",
			numBlocks: 10,
			archival:  true,
		},
		{
			name:      "non_archival_before_first_trie_commit",
			numBlocks: 10, // < commitInterval
		},
		{
			name:      "non_archival_after_trie_commit",
			numBlocks: commitInterval + 15, // ensure another settled block
		},
		{
			name:      "non_archival_commit_interval_exactly",
			numBlocks: commitInterval,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var srcDB database.Database
			srcHDB := saetest.NewHeightIndexDB()

			sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
			ctx, src := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
				srcDB = c.db
				c.logLevel = logging.Warn
				c.vmConfig.DBConfig.Archival = tt.archival
			}))

			for range tt.numBlocks {
				vmTime.advance(850 * time.Millisecond)
				b := src.runConsensusLoop(t, src.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: big.NewInt(1),
				}))
				require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
			}

			newDB := copyDB(t, srcDB)
			_, sut := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB.Clone()), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
				c.db = newDB
				c.logLevel = logging.Warn
				c.vmConfig.DBConfig.Archival = tt.archival
			}))

			requireConsensusCriticalBlocks(t, src, sut)

			if tt.archival {
				return
			}

			// For non-archival nodes, states outside [lastSettled, lastExecuted]
			// must not be accessible, except at CommitTrieDBEvery boundaries
			// where the settled state was written to disk.
			t.Run("unavailable_outside_window", func(t *testing.T) {
				lastSettled := sut.rawVM.last.settled.Load().NumberU64()
				committedHeight := saedb.LastCommittedTrieDBHeight(lastSettled, commitInterval)
				lastOnDisk, err := canonicalBlock(sut.rawVM.db, committedHeight)
				require.NoErrorf(t, err, "canonicalBlock(): %d", committedHeight)

				for i := sut.hooks.SettledHeight(lastOnDisk.Header()) + 1; i < lastSettled; i++ {
					ethB, err := canonicalBlock(sut.rawVM.db, i)
					require.NoErrorf(t, err, "canonicalBlock(%d)", i)
					b, err := blocks.New(ethB, nil, nil, sut.logger)
					require.NoErrorf(t, err, "blocks.New(): height %d", ethB.NumberU64())
					require.NoErrorf(t, b.RestoreExecutionArtefacts(sut.rawVM.db, sut.rawVM.xdb, sut.rawVM.exec.ChainConfig()), "%T.RestoreExecutionArtifacts(): %d", b, b.NumberU64())

					// If these states were available they would eventually
					// result in an OOM as the triedb leaked memory.
					root := b.PostExecutionStateRoot()
					_, err = sut.rawVM.exec.StateDB(root)
					want := testerr.As(func(got *trie.MissingNodeError) string {
						if got.NodeHash != root {
							return fmt.Sprintf("%T for hash %#x", got, root)
						}
						return ""
					})
					if diff := testerr.Diff(err, want); diff != "" {
						t.Errorf("%T.StateDB([post-execution root of block %d]) %s", sut.rawVM.exec, b.NumberU64(), diff)
					}
				}
			})
		})
	}
}

func requireConsensusCriticalBlocks(t *testing.T, src, sut *SUT) {
	t.Helper()

	t.Run("consensus_critical", func(t *testing.T) {
		if diff := cmp.Diff(src.rawVM.consensusCritical.m, sut.rawVM.consensusCritical.m, blocks.CmpOpt()); diff != "" {
			t.Errorf("%T.consensusCritical diff (-source +recovered):\n%s", src.rawVM, diff)
		}
		for _, b := range sut.rawVM.consensusCritical.m {
			root := b.PostExecutionStateRoot()
			_, err := sut.rawVM.exec.StateDB(root)
			assert.NoErrorf(t, err, "post-execution state root %#x of consensus-critical block[%d] with hash %#x", root, b.Height(), b.Hash())
		}
	})

	t.Run("last", func(t *testing.T) {
		for name, fn := range map[string](func(vm *VM) *blocks.Block){
			"accepted": func(vm *VM) *blocks.Block { return vm.last.accepted.Load() },
			"executed": func(vm *VM) *blocks.Block { return vm.exec.LastExecuted() },
			"settled":  func(vm *VM) *blocks.Block { return vm.last.settled.Load() },
		} {
			t.Run(name, func(t *testing.T) {
				got := fn(sut.rawVM)
				want := fn(src.rawVM)
				if diff := cmp.Diff(want, got, blocks.CmpOpt()); diff != "" {
					t.Errorf("(-want +got):\n%s", diff)
				}
			})
		}
	})
}

func copyDB(t *testing.T, srcDB database.Database) database.Database {
	t.Helper()

	newDB := memdb.New()
	it := srcDB.NewIterator()
	for it.Next() {
		require.NoErrorf(t, newDB.Put(it.Key(), it.Value()), "%T.Put() during database copy", newDB)
	}
	require.NoErrorf(t, it.Error(), "%T.Error() after database copy", it)

	return newDB
}
