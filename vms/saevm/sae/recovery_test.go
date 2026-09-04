// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
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
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// TestRecoverAfterCrash recovers from a copy of a running VM's database,
// taken without a clean shutdown.
func TestRecoverAfterCrash(t *testing.T) {
	t.Parallel()

	const (
		// blockTime is randomly selected to ensure multiple blocks are settled,
		// but allows gaps between settlement to check ancestry edge cases.
		blockTime      = 850 * time.Millisecond
		commitInterval = 16
	)

	sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

	var srcDB database.Database
	srcHDB := saetest.NewHeightIndexDB()
	ctx, src := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
		srcDB = c.db
		c.logLevel = logging.Warn
	}))

	// Build past the commit interval so the copied database holds both a
	// committed trie root and later, uncommitted states.
	for range commitInterval + 5 {
		vmTime.Advance(blockTime)
		b := src.runConsensusLoop(t, src.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &common.Address{},
			Gas:       params.TxGas,
			GasFeeCap: big.NewInt(1),
		}))
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	}

	newDB := saetest.CopyDB(t, srcDB) // note: src is still running, but concurrent safe
	sutCtx, sut := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB.Clone()), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
		c.db = newDB
		c.logLevel = logging.Warn
	}))

	requireConsensusCriticalBlocks(t, src, sut)

	t.Run("build_after_recovery", func(t *testing.T) {
		vmTime.Advance(blockTime)
		b := sut.runConsensusLoop(t)
		require.NoErrorf(t, b.WaitUntilExecuted(sutCtx), "%T.WaitUntilExecuted()", b)
	})
}

// TestRecoverWithBLOCKHASH restarts a VM whose accepted-but-uncommitted blocks
// carry transactions that read historical block hashes via the BLOCKHASH
// opcode.
func TestRecoverWithBLOCKHASH(t *testing.T) {
	t.Parallel()

	const (
		commitInterval = 16
		numBlocks      = commitInterval + 15
	)

	// A contract executing BLOCKHASH(NUMBER-2).
	contractAddr := common.Address{'b', 'l', 'o', 'c', 'k', 'h', 'a', 's', 'h'}
	contractCode := []byte{
		byte(vm.PUSH1), 2,
		byte(vm.NUMBER),
		byte(vm.SUB),
		byte(vm.BLOCKHASH),
	}
	withContract := options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc[contractAddr] = types.Account{
			Code:    contractCode,
			Balance: new(big.Int),
		}
	})

	timeOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

	var srcDB database.Database
	srcHDB := saetest.NewHeightIndexDB()
	ctx, src := newSUT(t, 1, timeOpt, withContract, withExecResultsDB(srcHDB), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
		srcDB = c.db
		c.logLevel = logging.Warn
	}))

	for range numBlocks {
		vmTime.Advance(850 * time.Millisecond)
		b := src.runConsensusLoop(t, src.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &contractAddr,
			Gas:       params.TxGas + 1_000,
			GasFeeCap: big.NewInt(1),
		}))
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	}
	src.close()

	// Recreating the VM replays all accepted blocks since the last trie
	// commit, re-executing the BLOCKHASH transactions.
	_, sut := newSUT(t, 1, timeOpt, withContract, withExecResultsDB(srcHDB.Clone()), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
		c.db = saetest.CopyDB(t, srcDB)
		c.logLevel = logging.Warn
	}))

	requireConsensusCriticalBlocks(t, src, sut)
}

func TestRecover(t *testing.T) {
	t.Parallel()

	const commitInterval = 16
	tests := []struct {
		name      string
		numBlocks int
		scheme    string // defaults to rawdb.HashScheme
		archival  bool
	}{
		{
			name:      "genesis",
			numBlocks: 0,
		},
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
		{
			name:      "archival_firewood",
			numBlocks: 10,
			scheme:    customrawdb.FirewoodScheme,
			archival:  true,
		},
		{
			name:      "non_archival_firewood_before_first_trie_commit",
			numBlocks: 2, // no new blocks settled yet
			scheme:    customrawdb.FirewoodScheme,
		},

		{
			name:      "non_archival_firewood_commit_interval_exactly",
			numBlocks: commitInterval,
			scheme:    customrawdb.FirewoodScheme,
		},
		{
			name:      "non_archival_firewood_beyond_revision_window",
			numBlocks: 3 * commitInterval, // evicts the oldest revisions from the in-memory window
			scheme:    customrawdb.FirewoodScheme,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var srcDB database.Database
			srcHDB := saetest.NewHeightIndexDB()
			tempDir := t.TempDir()

			sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
			ctx, src := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
				srcDB = c.db
				c.logLevel = logging.Warn
				c.vmConfig.DBConfig.Archival = tt.archival
				c.vmConfig.DBConfig.Scheme = tt.scheme
				c.dataDir = tempDir
			}))

			for range tt.numBlocks {
				vmTime.Advance(850 * time.Millisecond)
				b := src.runConsensusLoop(t, src.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: big.NewInt(1),
				}))
				require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
			}
			src.close()

			newDB := saetest.CopyDB(t, srcDB)
			_, sut := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB.Clone()), withCommitInterval(commitInterval), options.Func[sutConfig](func(c *sutConfig) {
				c.db = newDB
				c.logLevel = logging.Warn
				c.vmConfig.DBConfig.Archival = tt.archival
				c.vmConfig.DBConfig.Scheme = tt.scheme
				c.dataDir = tempDir
			}))

			requireConsensusCriticalBlocks(t, src, sut)

			// Firewood nodes have a rolling buffer of available recent states,
			// so it doesn't need to be managed directly.
			// Archival nodes store all states.
			if !tt.archival && tt.scheme != customrawdb.FirewoodScheme {
				// For non-archival, HashDB nodes, states outside
				// [lastSettled, lastExecuted] MUST NOT be accessible, except at
				// CommitTrieDBEvery boundaries where the settled state was
				// written to disk. Otherwise, the memory will leak.
				t.Run("unavailable_outside_window", func(t *testing.T) {
					lastSettled := sut.rawVM.last.settled.Load().NumberU64()
					committedHeight := saedb.LastCommittedTrieDBHeight(lastSettled, commitInterval)
					lastOnDisk, err := canonicalBlock(sut.rawVM.db, committedHeight)
					require.NoErrorf(t, err, "canonicalBlock(): %d", committedHeight)

					for i := sut.hooks.SettledBy(lastOnDisk.Header()).Height + 1; i < lastSettled; i++ {
						ethB, err := canonicalBlock(sut.rawVM.db, i)
						require.NoErrorf(t, err, "canonicalBlock(%d)", i)
						b, err := blocks.RestoreSettledBlock(ethB, sut.hooks, sut.logger, sut.db, sut.rawVM.xdb, sut.rawVM.exec.ChainConfig())
						require.NoErrorf(t, err, "RestoreSettledBlock(%d)", i)

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
			}

			t.Run("settle_after_recovery", func(t *testing.T) {
				vmTime.AdvanceToSettle(ctx, t, sut.lastAcceptedBlock(t))
				// use old wallet for correct nonce.
				tx := src.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: big.NewInt(1),
				})
				b := sut.runConsensusLoop(t, tx)
				require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
			})
		})
	}
}

// TestRecoverSnapshotAfterShutdown cleanly restarts a VM. The snapshot
// persisted at shutdown MUST be loaded as-is and MUST verify against the
// recovered state.
//
// A snapshot that fails to load is regenerated, which may take hours on a
// mainnet-sized state. Until that finishes, state reads and state-sync serving
// fall back to the far slower trie. Clean restarts are routine and should not
// result in a significant performance reduction.
func TestRecoverSnapshotAfterShutdown(t *testing.T) {
	t.Parallel()

	timeOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	sharedOpts := []options.Option[sutConfig]{
		timeOpt,
		withSnapshot(),
	}
	db := memdb.New()
	xdb := saetest.NewHeightIndexDB()
	ctx, sut := newSUT(t, 1, append(sharedOpts, withExecResultsDB(xdb), options.Func[sutConfig](func(c *sutConfig) {
		c.db = db
	}))...)
	// The genesis snapshot generates in the background. Waiting for it keeps
	// the blocks below from interrupting generation.
	requireSnapshotEventuallyVerified(t, sut)

	newBlock := func(t *testing.T) *blocks.Block {
		t.Helper()
		b := sut.runConsensusLoop(t, sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &common.Address{},
			Gas:       params.TxGas,
			GasFeeCap: big.NewInt(1),
		}))
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
		vmTime.AdvanceToSettle(ctx, t, b)
		return b
	}

	settled := newBlock(t)
	settler := newBlock(t)
	require.Equal(t, settled.PostExecutionStateRoot(), settler.SettledStateRoot(), "the settled root should have advanced")

	sut.verifySnapshot(t)
	snaps := sut.rawVM.exec.Snapshot()
	sut.close()

	// Closing flattens every diff layer into the disk layer before persisting
	// it, so the root MUST be read afterwards.
	want := snaps.DiskRoot()
	require.Equal(t, settler.SettledStateRoot(), want, "shutting down should flush the last settled state to disk")

	_, sut = newSUT(t, 1, append(sharedOpts, withExecResultsDB(xdb.Clone()), options.Func[sutConfig](func(c *sutConfig) {
		c.db = saetest.CopyDB(t, db)
	}))...)

	snaps = sut.rawVM.exec.Snapshot()
	require.NotNilf(t, snaps, "%T.Snapshot()", sut.rawVM.exec)
	require.Equalf(t, want, snaps.DiskRoot(), "%T.DiskRoot() after restart MUST be the root persisted at shutdown", snaps)
	sut.verifySnapshot(t)
}

// A failure to open the execution-results database must abort [VM]
// initialization with an error that unwraps to the cause.
func TestNewVMExecutionResultsDBError(t *testing.T) {
	t.Parallel()

	errInjected := errors.New("injected ExecutionResultsDB failure")
	_, err := tryNewSUT(t, 0, options.Func[sutConfig](func(c *sutConfig) {
		c.hooks = hookstest.NewStub(100e6, hookstest.WithExecutionResultsDBFn(func(string) (saetypes.ExecutionResults, error) {
			return saetypes.ExecutionResults{}, errInjected
		}))
		c.logLevel = logging.Warn
	}))
	require.ErrorIs(t, err, errInjected, "Initialize() with failing ExecutionResultsDB")
}

func TestNewVMIncompatibleExecutionResults(t *testing.T) {
	t.Parallel()

	sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

	var (
		srcDB      database.Database
		srcGenesis core.Genesis
	)
	ctx, src := newSUT(t, 1, sutOpt, options.Func[sutConfig](func(c *sutConfig) {
		srcDB = c.db
		srcGenesis = c.genesis
		c.logLevel = logging.Warn
	}))

	b := src.runConsensusLoop(t)
	vmTime.AdvanceToSettle(ctx, t, b)
	settler := src.runConsensusLoop(t)
	require.NoErrorf(t, settler.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", settler)
	require.Equal(t, b.Height(), src.rawVM.last.settled.Load().Height(), "settled height after accepting settler")

	_, err := tryNewSUT(t, 0, options.Func[sutConfig](func(c *sutConfig) {
		c.hooks = hookstest.NewStub(100e6, hookstest.WithExecutionResultsDBFn(func(string) (saetypes.ExecutionResults, error) {
			return saetest.NewExecutionResultsDB(), nil
		}))
		c.db = saetest.CopyDB(t, srcDB)
		c.genesis = srcGenesis
		c.logLevel = logging.Warn
	}))
	require.ErrorIs(t, err, blocks.ErrMissingExecutionResults, "Initialize() with incompatible execution-results DB")
}

func requireConsensusCriticalBlocks(t *testing.T, src, sut *SUT) {
	t.Helper()

	opts := cmp.Options{
		blocks.CmpOpt(),
		blocks.IgnoreLastSettledExecutionArtefacts(t),
	}

	t.Run("consensus_critical", func(t *testing.T) {
		if diff := cmp.Diff(src.rawVM.consensusCritical.m, sut.rawVM.consensusCritical.m, opts); diff != "" {
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
				if diff := cmp.Diff(want, got, opts); diff != "" {
					t.Errorf("(-want +got):\n%s", diff)
				}
			})
		}
	})
}
