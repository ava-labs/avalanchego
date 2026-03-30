// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocks

import (
	"context"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/gastime"
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

	ethB := types.NewBlock(
		&types.Header{
			Number: big.NewInt(1),
			Time:   42,
		},
		txs,
		nil, nil, // uncles, receipts
		saetest.TrieHasher(),
	)
	db := rawdb.NewMemoryDatabase()
	rawdb.WriteBlock(db, ethB)
	xdb := saetest.NewExecutionResultsDB()

	settles := newBlock(t, newEthBlock(0, 0, nil), nil, nil)
	tm := mustNewGasTime(t, time.Unix(0, 0), 1, 0, gastime.DefaultGasPriceConfig())
	settles.markExecutedForTests(t, db, xdb, tm)
	b := newBlock(t, ethB, nil, settles)

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
	require.NoError(t, fromDB.RestoreExecutionArtefacts(db, xdb, saetest.ChainConfig()), "RestoreExecutionArtefacts()")
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
				rec := saetest.NewLogRecorder(logging.Warn)
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

// selfAsHasher adds a Hash() method to a common.Hash, returning itself.
type selfAsHasher common.Hash

func (h selfAsHasher) Hash() common.Hash { return common.Hash(h) }

func mustNewGasTime(tb testing.TB, at time.Time, target, excess gas.Gas, gasPriceConfig saetypes.GasPriceConfig) *gastime.Time {
	tb.Helper()
	tm, err := gastime.New(at, target, excess, gasPriceConfig)
	require.NoError(tb, err, "gastime.New()")
	return tm
}
