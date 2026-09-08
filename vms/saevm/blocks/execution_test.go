// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"context"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/hook/hookstest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	saetypes "github.com/ava-labs/avalanchego/vms/saevm/types"
)

// markExecutedForTests calls [Block.MarkExecuted] with zero-value
// post-execution artefacts (other than the gas time).
func (b *Block) markExecutedForTests(tb testing.TB, db ethdb.Database, xdb saetypes.ExecutionResults, tm *gastime.Time) {
	tb.Helper()
	require.NoError(tb, b.MarkExecuted(db, xdb, tm, time.Time{}, new(big.Int), nil, common.Hash{}, new(atomic.Pointer[Block])), "MarkExecuted()")
}

func TestMarkExecuted(t *testing.T) {
	const gasPrice = 100
	txs := make(types.Transactions, 10)
	for i := range txs {
		txs[i] = types.NewTx(&types.LegacyTx{
			Nonce:    uint64(i), //#nosec G115 -- Won't overflow
			GasPrice: big.NewInt(gasPrice),
			Gas:      params.TxGas,
			To:       &common.Address{},
		})
	}

	db := rawdb.NewMemoryDatabase()
	xdb := saetest.NewExecutionResultsDB()
	tm := mustNewGasTime(t, time.Unix(0, 0), 1, 0, gastime.DefaultGasPriceConfig())

	settles := newBlock(t, newSynchronousEthBlock(t, 1, 0, nil), nil, nil)
	settles.markExecutedForTests(t, db, xdb, tm)

	parent := newBlock(t, newEthBlock(t, 2, 10, settles.EthBlock(), settles), settles, settles)

	ethB, err := hookstest.BuildBlock(
		&types.Header{
			Number:     big.NewInt(3),
			Time:       42,
			ParentHash: parent.Hash(),
		},
		nil, // blockContext
		txs,
		nil, // receipts
		nil, // ops
		hook.Settled{Height: settles.Height()},
	)
	require.NoError(t, err, "hookstest.BuildBlock(...)")
	rawdb.WriteBlock(db, ethB)
	b := newBlock(t, ethB, parent, settles)

	t.Run("before_MarkExecuted", func(t *testing.T) {
		require.False(t, b.Executed(), "Executed()")
		require.NoError(t, b.CheckInvariants(NotExecuted), "CheckInvariants(NotExecuted)")

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()
		require.ErrorIs(t, b.WaitUntilExecuted(ctx), context.DeadlineExceeded, "WaitUntilExecuted()")
	})

	gasTime := mustNewGasTime(t, time.Unix(42, 0), 1e6, 42, gastime.DefaultGasPriceConfig())
	wallTime := time.Unix(42, 100)
	stateRoot := common.Hash{'s', 't', 'a', 't', 'e'}
	baseFee := uint256.NewInt(314159)
	var (
		receipts      types.Receipts
		cumulativeGas uint64
	)
	for i, tx := range txs {
		cumulativeGas += params.TxGas
		receipts = append(receipts, &types.Receipt{
			Type:              tx.Type(),
			Status:            types.ReceiptStatusSuccessful,
			TxHash:            tx.Hash(),
			GasUsed:           params.TxGas,
			CumulativeGasUsed: cumulativeGas,
			EffectiveGasPrice: big.NewInt(gasPrice),
			BlockHash:         ethB.Hash(),
			BlockNumber:       new(big.Int).Set(ethB.Number()),
			TransactionIndex:  uint(i), //#nosec G115 -- Won't overflow
		})
	}
	lastExecuted := new(atomic.Pointer[Block])
	require.NoError(t, b.MarkExecuted(db, xdb, gasTime, wallTime, baseFee.ToBig(), receipts, stateRoot, lastExecuted), "MarkExecuted()")

	fromDB := newBlock(t, b.EthBlock(), b.ParentBlock(), b.LastSettled())
	require.NoErrorf(t, fromDB.RestoreExecutionArtefacts(db, xdb, saetest.ChainConfig()), "%T.RestoreExecutionArtefacts()", fromDB)
	tests := []struct {
		name           string
		isLastExecuted bool
		block          *Block
	}{
		{
			name:           "after_MarkExecuted",
			isLastExecuted: true,
			block:          b,
		},
		{
			name:           "after_ReloadExecutionResults",
			isLastExecuted: false,
			block:          fromDB,
		},
	}
	for _, tt := range tests {
		b := tt.block
		t.Run(tt.name, func(t *testing.T) {
			require.True(t, b.Executed(), "Executed()")
			assert.NoError(t, b.CheckInvariants(Executed), "CheckInvariants(Executed)")

			require.NoError(t, b.WaitUntilExecuted(t.Context()), "WaitUntilExecuted()")

			assert.Zero(t, b.ExecutedByGasTime().Compare(gasTime.Time), "ExecutedByGasTime().Compare([original input])")
			assert.Zero(t, b.ExecutedBaseFee().Cmp(baseFee), "ExecutedBaseFee().Cmp([original input])")
			assert.Empty(t, cmp.Diff(receipts, b.Receipts(), cmputils.Receipts(), cmputils.NilSlicesAreEmpty[[]*types.Log]()), "Receipts()")

			assert.Equal(t, stateRoot, b.PostExecutionStateRoot(), "PostExecutionStateRoot()") // i.e. this block
			// Although not directly relevant to MarkExecuted, demonstrate that the
			// two notions of a state root are in fact different.
			assert.Equal(t, settles.EthBlock().Root(), b.SettledStateRoot(), "SettledStateRoot()") // i.e. the block this block settles
			assert.NotEqual(t, b.SettledStateRoot(), b.PostExecutionStateRoot(), "PostExecutionStateRoot() != SettledStateRoot()")

			if tt.isLastExecuted {
				assert.Equal(t, b, lastExecuted.Load(), "Atomic pointer to last-executed block")
			}

			t.Run("MarkExecuted_again", func(t *testing.T) {
				rec := loggingtest.NewRecorder(logging.Warn)
				b.log = rec
				require.ErrorIs(t, b.MarkExecuted(db, xdb, gasTime, wallTime, baseFee.ToBig(), receipts, stateRoot, lastExecuted), errMarkBlockExecutedAgain)
				// The database's head block might have been corrupted so this MUST
				// be a fatal action.
				assert.Len(t, rec.At(logging.Fatal), 1, "FATAL logs")
			})
		})
	}

	t.Run("database", func(t *testing.T) {
		t.Run("head_block", func(t *testing.T) {
			for fn, got := range map[string]interface{ Hash() common.Hash }{
				"ReadHeadBlockHash":  selfAsHasher(rawdb.ReadHeadBlockHash(db)),
				"ReadHeadHeaderHash": selfAsHasher(rawdb.ReadHeadHeaderHash(db)),
				"ReadHeadBlock":      rawdb.ReadHeadBlock(db),
				"ReadHeadHeader":     rawdb.ReadHeadHeader(db),
			} {
				t.Run("rawdb."+fn, func(t *testing.T) {
					require.NotNil(t, got)
					assert.Equalf(t, b.Hash(), got.Hash(), "rawdb.%s()", fn)
				})
			}
		})
	})
}

// errAll requires the error to satisfy every `want`. [testerr] provides only
// primitive matchers, leaving their composition to the caller.
func errAll(wants ...testerr.Want) testerr.Want {
	return testerr.Func(func(got error) string {
		for _, w := range wants {
			if diff := w.ErrDiff(got); diff != "" {
				return diff
			}
		}
		return ""
	})
}

// errIsNot requires that the error does NOT wrap `target`; a nil error
// trivially satisfies this.
func errIsNot(target error) testerr.Want {
	return testerr.Func(func(got error) string {
		if errors.Is(got, target) {
			return testerr.DiffMessage(got, "error that is not %v", target)
		}
		return ""
	})
}

func TestRestoreExecutionArtefacts(t *testing.T) {
	const height = 2
	asynchronous := hook.Settled{Height: height - 1}

	putCorruptResults := func(t *testing.T, _ ethdb.Database, xdb saetypes.ExecutionResults, _ hook.Points, ethB *types.Block) {
		t.Helper()
		require.NoErrorf(t, xdb.Put(ethB.NumberU64(), []byte("not canoto")), "%T.Put()", xdb)
	}

	validGasTime := func(hdr *types.Header, hooks hook.Points) *gastime.Time {
		target, cfg := hooks.GasConfigAfter(hdr)
		return mustNewGasTime(t, hooks.BlockTime(hdr), target, gas.Price(hdr.BaseFee.Uint64()), cfg)
	}

	tests := []struct {
		name             string
		settled          hook.Settled
		txs              []*types.Transaction
		hookOpts         []hookstest.HookOption
		checkSynchronous bool
		setupDBs         func(t *testing.T, db ethdb.Database, xdb saetypes.ExecutionResults, hooks hook.Points, ethB *types.Block)
		wantErr          testerr.Want
	}{
		{
			name:    "asynchronous_missing_execution_results",
			settled: asynchronous,
			wantErr: testerr.Is(ErrMissingExecutionResults),
		},
		{
			name:     "asynchronous_corrupt_execution_results",
			settled:  asynchronous,
			setupDBs: putCorruptResults,
			wantErr:  testerr.Is(ErrMissingExecutionResults),
		},
		{
			name:    "asynchronous_unreadable_execution_results",
			settled: asynchronous,
			setupDBs: func(t *testing.T, _ ethdb.Database, xdb saetypes.ExecutionResults, _ hook.Points, _ *types.Block) {
				t.Helper()
				require.NoErrorf(t, xdb.Close(), "%T.Close()", xdb)
			},
			wantErr: errAll(
				testerr.Is(ErrMissingExecutionResults),
				testerr.Is(database.ErrClosed),
			),
		},
		{
			name:    "empty_receipts_no_error",
			settled: asynchronous,
			txs:     []*types.Transaction{types.NewTx(&types.LegacyTx{Gas: params.TxGas})},
			setupDBs: func(t *testing.T, db ethdb.Database, xdb saetypes.ExecutionResults, hooks hook.Points, ethB *types.Block) {
				t.Helper()
				tm := validGasTime(ethB.Header(), hooks)
				newBlock(t, ethB, nil, nil).markExecutedForTests(t, db, xdb, tm)
			},
		},
		{
			name:    "missing_receiptis",
			settled: asynchronous,
			txs:     []*types.Transaction{types.NewTx(&types.LegacyTx{Gas: params.TxGas})},
			setupDBs: func(t *testing.T, db ethdb.Database, xdb saetypes.ExecutionResults, hooks hook.Points, ethB *types.Block) {
				t.Helper()
				tm := validGasTime(ethB.Header(), hooks)
				b := newBlock(t, ethB, nil, nil)
				receipts := types.Receipts{&types.Receipt{Status: 8}}
				require.NoErrorf(t, b.MarkExecuted(db, xdb, tm, time.Time{}, new(big.Int), receipts, common.Hash{}, new(atomic.Pointer[Block])), "%T.MarkExecuted()", b)
				rawdb.DeleteReceipts(db, ethB.Hash(), ethB.NumberU64())
			},
			wantErr: errAll(
				errIsNot(ErrMissingExecutionResults),
				testerr.Contains("deriving receipt fields"),
			),
		},
		{
			name:             "synchronous_ignores_execution_results",
			setupDBs:         putCorruptResults,
			checkSynchronous: true,
		},
		{
			name:     "synchronous_gas_time_error",
			hookOpts: []hookstest.HookOption{hookstest.WithGasPriceConfig(gastime.GasPriceConfig{})},
			wantErr:  testerr.Is(ErrMissingExecutionResults),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ethB, err := hookstest.BuildBlock(
				&types.Header{
					Number:  big.NewInt(height),
					BaseFee: big.NewInt(1),
					Time:    42,
				},
				nil, tt.txs, nil, nil, tt.settled,
			)
			require.NoError(t, err, "hookstest.BuildBlock()")

			hooks := hookstest.NewStub(1e6, tt.hookOpts...)
			db := rawdb.NewMemoryDatabase()
			xdb := saetest.NewExecutionResultsDB()
			if tt.setupDBs != nil {
				tt.setupDBs(t, db, xdb, hooks, ethB)
			}

			b, err := New(ethB, nil, nil, hooks, loggingtest.New(t, logging.Warn))
			require.NoError(t, err, "New()")
			err = b.RestoreExecutionArtefacts(db, xdb, saetest.ChainConfig())
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("%T.RestoreExecutionArtefacts() %s", b, diff)
			}
			if !tt.checkSynchronous {
				return
			}

			synchronous := hook.Synchronous(hooks, ethB.Header())
			require.NoErrorf(t, b.CheckInvariants(Executed), "%T.CheckInvariants(Executed)", b)
			require.Equalf(t, synchronous, b.Synchronous(), "%T.Synchronous()", b)
			require.Falsef(t, b.Settled(), "%T.Settled()", b)
			require.Equalf(t, synchronous, b == b.LastSettled(), "%T is its own LastSettled()", b)
		})
	}
}

// selfAsHasher adds a Hash() method to a common.Hash, returning itself.
type selfAsHasher common.Hash

func (h selfAsHasher) Hash() common.Hash { return common.Hash(h) }

func mustNewGasTime(tb testing.TB, at time.Time, target gas.Gas, price gas.Price, gasPriceConfig gastime.GasPriceConfig) *gastime.Time {
	tb.Helper()
	tm, err := gastime.New(at, target, price, gasPriceConfig)
	require.NoError(tb, err, "gastime.New()")
	return tm
}
