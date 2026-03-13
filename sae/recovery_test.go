// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"math/big"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/cmputils"
	saeparams "github.com/ava-labs/strevm/params"
	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saetest"
)

func TestRecoverFromDatabase(t *testing.T) {
	sutOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

	var srcDB database.Database
	srcHDB := saetest.NewHeightIndexDB()
	ctx, src := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB), options.Func[sutConfig](func(c *sutConfig) {
		srcDB = c.db
		c.logLevel = logging.Warn
	}))
	srcCtx := ctx

	rng := rand.New(rand.NewPCG(0, 0)) //nolint:gosec // Deterministic replay for tests

	for final := false; !final; {
		// We need to test rebuilding from trie roots reflecting (a) the last
		// synchronous block; (b) some committed state root; and (c) a few
		// blocks before/after the thresholds. Everything in between is merely
		// to advance the block number so is treated as a "quick" loop
		// iteration.
		last := src.lastAcceptedBlock(t)
		height := last.Height()
		quick := height < saedb.CommitTrieDBEvery && src.rawVM.last.settled.Load().Height() > 1
		final = height > saedb.CommitTrieDBEvery

		if !quick {
			src.mustSendTx(t, src.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
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
			newDB := memdb.New()
			it := srcDB.NewIterator()
			for it.Next() {
				require.NoError(t, newDB.Put(it.Key(), it.Value()))
			}
			require.NoError(t, it.Error())

			sutCtx, sut := newSUT(t, 1, sutOpt, withExecResultsDB(srcHDB.Clone()), options.Func[sutConfig](func(c *sutConfig) {
				c.db = newDB
				c.logLevel = logging.Warn
			}))

			if final {
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
							sys.mustSendTx(t, tx)
							b := sys.runConsensusLoop(t)
							require.Len(t, b.Transactions(), 1)
							require.NoError(t, b.WaitUntilExecuted(sys.ctx))
						})
					}
				})
				if t.Failed() {
					t.FailNow()
				}
			}

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

			if diff := cmp.Diff(src.rawVM.consensusCritical.m, sut.rawVM.consensusCritical.m, blocks.CmpOpt()); diff != "" {
				t.Errorf("%T.inConsensus diff (-source +recovered):\n%s", src.rawVM, diff)
			}
		})
	}
}
