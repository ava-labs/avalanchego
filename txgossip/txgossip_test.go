// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/eth"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/holiman/uint256"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/blocks/blockstest"
	"github.com/ava-labs/strevm/cmputils"
	"github.com/ava-labs/strevm/hook/hookstest"
	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saetest"
	"github.com/ava-labs/strevm/saexec"
	"github.com/ava-labs/strevm/txgossip/txgossiptest"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(
		m,
		goleak.IgnoreCurrent(),
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core/state/snapshot.(*diskLayer).generate"),
		// Even with a call to [txpool.TxPool.Close], this leaks. We don't
		// expect to open and close multiple pools, so it's ok to ignore.
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core/txpool.(*TxPool).loop.func2"),
	)
}

// SUT is the system under test, primarily the [Set].
type SUT struct {
	*Set
	chain  *blockstest.ChainBuilder
	wallet *saetest.Wallet
	exec   *saexec.Executor
}

func newWallet(tb testing.TB, numAccounts uint) *saetest.Wallet {
	tb.Helper()
	signer := types.LatestSigner(saetest.ChainConfig())
	return saetest.NewUNSAFEWallet(tb, numAccounts, signer)
}

func newSUT(t *testing.T, numAccounts uint) SUT {
	t.Helper()
	logger := saetest.NewTBLogger(t, logging.Warn)

	wallet := newWallet(t, numAccounts)
	config := saetest.ChainConfig()

	db := rawdb.NewMemoryDatabase()
	xdb := saetest.NewExecutionResultsDB()
	genesis := blockstest.NewGenesis(t, db, xdb, config, saetest.MaxAllocFor(wallet.Addresses()...))
	chain := blockstest.NewChainBuilder(config, genesis)
	src := blocks.Source(chain.GetBlock)

	exec, err := saexec.New(genesis, src.AsHeaderSource(), config, db, xdb, saedb.Config{}, hookstest.NewStub(1e6), logger)
	require.NoError(t, err, "saexec.New()")
	t.Cleanup(func() {
		require.NoErrorf(t, exec.Close(), "%T.Close()", exec)
	})

	bc := NewBlockChain(exec, src.AsEthBlockSource())
	pool := newTxPool(t, bc)
	set, err := NewSet(logger, pool, gossip.BloomSetConfig{})
	require.NoError(t, err, "NewSet()")
	t.Cleanup(func() {
		assert.NoErrorf(t, pool.Close(), "%T.Close()", pool)
	})

	return SUT{
		Set:    set,
		chain:  chain,
		wallet: wallet,
		exec:   exec,
	}
}

func newTxPool(t *testing.T, bc BlockChain) *txpool.TxPool {
	t.Helper()

	config := legacypool.DefaultConfig // copies
	config.Journal = filepath.Join(t.TempDir(), "transactions.rlp")
	subs := []txpool.SubPool{legacypool.New(config, bc)}

	p, err := txpool.New(1, bc, subs)
	require.NoError(t, err, "txpool.New()")
	return p
}

func TestExecutorIntegration(t *testing.T) {
	ctx := t.Context()

	const numAccounts = 3
	s := newSUT(t, numAccounts)

	rng := rand.New(rand.NewPCG(0, 0)) //nolint:gosec // Reproducibility is useful in tests

	const txPerAccount = 5
	const numTxs = numAccounts * txPerAccount
	signedTxs := make([]*types.Transaction, 0, numTxs)
	for range txPerAccount {
		for i := range numAccounts {
			signedTxs = append(signedTxs, s.wallet.SetNonceAndSign(t, i, &types.DynamicFeeTx{
				To:        &common.Address{},
				Gas:       params.TxGas,
				GasFeeCap: big.NewInt(100),
				GasTipCap: big.NewInt(1 + rng.Int64N(3)),
			}))
		}
	}

	for _, tx := range signedTxs {
		require.NoErrorf(t, s.Add(Transaction{tx}), "%T.Add()", s.set)
	}
	txgossiptest.WaitUntilPending(t, ctx, s.Pool, signedTxs...)

	t.Run("Iterate_after_Add", func(t *testing.T) {
		require.Lenf(t, slices.Collect(s.Iterate), numTxs, "slices.Collect(%T.Iterate)", s.Set)
	})
	if t.Failed() {
		t.FailNow()
	}

	var (
		txs  types.Transactions
		last *LazyTransaction
	)
	for _, ltx := range s.TransactionsByPriority(txpool.PendingFilter{}) {
		tx, ok := ltx.Resolve()
		require.True(t, ok, "%T.Resolve() `ok` on tx returned by %T.TransactionsByPriority()", ltx, s)
		txs = append(txs, tx)

		t.Run("priority_ordering", func(t *testing.T) {
			defer func() { last = ltx }()

			switch {
			case last == nil:
			case ltx.Sender == last.Sender:
				require.Equal(t, last.Tx.Nonce()+1, ltx.Tx.Nonce(), "incrementing nonce for same sender")
			case ltx.GasTipCap.Eq(last.GasTipCap):
				require.True(t, last.Time.Before(ltx.Time), "equal gas tips ordered by first seen")
			default:
				require.Greater(t, last.GasTipCap.Uint64(), ltx.GasTipCap.Uint64(), "larger gas tips first")
			}
		})
	}
	require.Lenf(t, txs, numTxs, "%T.TransactionsByPriority()", s.Set)
	require.Equalf(t, numTxs, s.Len(), "%T.Len()", s.Set)

	b := s.chain.NewBlock(t, txs)
	require.NoErrorf(t, s.exec.Enqueue(ctx, b), "%T.Enqueue([txs from %T.TransactionsByPriority()])", s.exec, s.Set)

	// After the block is executed, a [core.ChainHeadEvent] triggers a reorg
	// in the mempool. We can't be explicitly notified when the reorg completes,
	// so polling is necessary.
	assert.EventuallyWithTf(
		t, func(c *assert.CollectT) {
			assert.Zerof(c, s.Len(), "%T.Len()", s.Set)
			assert.Emptyf(c, slices.Collect(s.Iterate), "slices.Collect(%T.Iterate)", s.Set)
			for _, tx := range txs {
				assert.Falsef(c, s.Has(ids.ID(tx.Hash())), "%T.Has(%#x)", s.Set, tx.Hash())
			}
		},
		2*time.Second, 10*time.Millisecond,
		"empty %T after transactions included in a block", s.Set,
	)

	t.Run("block_execution", func(t *testing.T) {
		// The above test for nonce ordering only runs if the same sender has 2
		// consecutive transactions. Successful execution demonstrates correct
		// nonce ordering across the board.
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)

		for i, r := range b.Receipts() {
			assert.Equalf(t, types.ReceiptStatusSuccessful, r.Status, "%T[%d].Status", r, i)
		}
	})
}

func TestP2PIntegration(t *testing.T) {
	w := newWallet(t, 1)
	txViaGossip := Transaction{w.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &common.Address{},
		Gas:      params.TxGas,
		GasPrice: big.NewInt(1),
	})}
	txViaRPC := Transaction{w.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &common.Address{},
		Gas:      params.TxGas,
		GasPrice: big.NewInt(1),
	})}
	bothTxs := []Transaction{txViaGossip, txViaRPC}
	t.Logf("Tx via gossip: %#x / %v", txViaGossip.Hash(), txViaGossip.GossipID())
	t.Logf("   Tx via RPC: %#x / %v", txViaRPC.Hash(), txViaRPC.GossipID())
	txLabel := func(tx Transaction) string {
		switch h := tx.Hash(); h {
		case txViaGossip.Hash():
			return "[tx via gossip]"
		case txViaRPC.Hash():
			return "[tx via RPC]"
		default:
			return fmt.Sprintf("[unknown tx %#x]", h)
		}
	}

	reg := prometheus.NewRegistry()
	metrics, err := gossip.NewMetrics(reg, "")
	require.NoError(t, err, "gossip.NewMetrics()")

	type pushOrPull bool
	const (
		push pushOrPull = true
		pull pushOrPull = false
	)

	tests := []struct {
		name        string
		dir         pushOrPull
		wantRecvTxs []Transaction
	}{
		{
			name: "push",
			dir:  push,
			wantRecvTxs: []Transaction{
				// [txViaGossip] MUST NOT be propagated further because this
				// will Snow-ball and make Stephen sad when he's woken from his
				// beauty sleep.
				txViaRPC,
			},
		},
		{
			name:        "pull",
			dir:         pull,
			wantRecvTxs: bothTxs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := saetest.NewTBLogger(t, logging.Debug)
			ctx := logger.CancelOnError(t.Context())

			sendID := ids.GenerateTestNodeID()
			recvID := ids.GenerateTestNodeID()
			send := newSUT(t, 1)
			// Although the receiving mempool doesn't need to sign transactions, create
			// the same (deterministic) account so it has non-zero balance otherwise the
			// mempool will reject it.
			recv := newSUT(t, 1)

			client := p2ptest.NewClient(
				t, ctx,
				recvID, gossip.NewHandler(logger, Marshaller{}, recv.Set, metrics, math.MaxInt),
				sendID, gossip.NewHandler(logger, Marshaller{}, send.Set, metrics, math.MaxInt),
			)

			var gossiper gossip.Gossiper
			switch tt.dir {
			case pull:
				gossiper = gossip.NewPullGossiper(logger, Marshaller{}, recv.Set, client, metrics, 1)

			case push:
				branch := gossip.BranchingFactor{Peers: 1}
				g, err := gossip.NewPushGossiper(
					Marshaller{},
					send.Set,
					&stubPeers{[]ids.NodeID{recvID}},
					client,
					metrics,
					branch, branch,
					0, 1<<20, time.Millisecond,
				)
				require.NoError(t, err, "%T.NewPushGossiper()")
				gossiper = g

				// NOTE: if reviewing this test to understand how to use the
				// package, this is an important step.
				send.RegisterPushGossiper(g)
			}

			require.NoErrorf(t, send.Add(txViaGossip), "%T.Add()", send.Set)
			require.NoErrorf(t, send.SendTx(ctx, txViaRPC.Transaction), "%T.SendTx()", send.Set)
			txgossiptest.WaitUntilPending(t, ctx, send.Pool, txViaRPC.Transaction, txViaGossip.Transaction)

			t.Run("confirm_setup", func(t *testing.T) {
				for _, tx := range bothTxs {
					lbl := txLabel(tx)
					assert.Truef(t, send.Has(tx.GossipID()), "sending %T.Has(%s)", send.Set, lbl)
				}
				assert.Emptyf(t, slices.Collect(recv.Iterate), "receiving %T.Iterate()", recv.Set)
			})
			if t.Failed() {
				t.FailNow()
			}

			// This uses assert instead of require because even if it fails,
			// knowing if any gossip has been successful (and which) was useful
			// for debugging.
			assert.EventuallyWithTf(
				t, func(c *assert.CollectT) {
					require.NoError(t, gossiper.Gossip(ctx))
					// Check for the tx from RPC as this is received regardless
					// of push or pull semantics.
					require.True(c, recv.Has(txViaRPC.GossipID()))
				},
				3*time.Second, 10*time.Millisecond,
				"Receiving %T.Has([tx via RPC])", recv.Set,
			)

			t.Run("Has", func(t *testing.T) {
				for _, tx := range tt.wantRecvTxs {
					assert.Truef(t, recv.Has(tx.GossipID()), "receiving %T.Has(%s)", recv.Set, txLabel(tx))
				}
			})
			t.Run("Iterate", func(t *testing.T) {
				got := slices.Collect(recv.Iterate)
				if diff := cmp.Diff(tt.wantRecvTxs, got, cmputils.TransactionsByHash()); diff != "" {
					t.Errorf("slices.Collect(%T.Iterate) diff (-want +got):\n%s", send.Set, diff)
				}
			})
		})
	}
}

type stubPeers struct {
	ids []ids.NodeID
}

var _ interface {
	p2p.NodeSampler
	p2p.ValidatorSubset
} = (*stubPeers)(nil)

func (p *stubPeers) Sample(context.Context, int) []ids.NodeID {
	return p.ids
}

func (p *stubPeers) Top(context.Context, float64) []ids.NodeID {
	return p.ids
}

func TestAPIBackendSendTxSignatureMatch(_ *testing.T) {
	// It's surprisingly difficult to get a concise compile-time guarantee that
	// a single method in an interface is implemented!
	var b ethapi.Backend = (*eth.EthAPIBackend)(nil)
	fn := b.SendTx //nolint:ineffassign,staticcheck
	fn = (*Set)(nil).SendTx
	_ = fn
}

func FuzzEffectiveGasTip(f *testing.F) {
	// The goal of these seeds is to exercise all possible orderings of fee cap,
	// tip cap and base fee, also including a nil base fee.
	ints := []uint64{0, 1, 2}
	for _, feeCap := range ints {
		for _, tipCap := range ints {
			for _, baseFee := range ints {
				for _, nilBase := range []bool{true, false} {
					const z = uint64(0)
					f.Add(nilBase, feeCap, z, z, z, tipCap, z, z, z, baseFee, z, z, z)
				}
			}
		}
	}

	f.Fuzz(func(t *testing.T, nilB bool, f0, f1, f2, f3, t0, t1, t2, t3, b0, b1, b2, b3 uint64) {
		feeCap := (*uint256.Int)(&[4]uint64{f0, f1, f2, f3})
		tipCap := (*uint256.Int)(&[4]uint64{t0, t1, t2, t3})
		var (
			baseFee    *uint256.Int
			bigBaseFee *big.Int
		)
		if !nilB {
			baseFee = (*uint256.Int)(&[4]uint64{b0, b1, b2, b3})
			bigBaseFee = baseFee.ToBig()
		}

		tx := types.NewTx(&types.DynamicFeeTx{
			GasFeeCap: feeCap.ToBig(),
			GasTipCap: tipCap.ToBig(),
		})
		want, err := tx.EffectiveGasTip(bigBaseFee)
		if errors.Is(err, types.ErrGasFeeCapTooLow) {
			want = big.NewInt(0)
		} else {
			require.NoErrorf(t, err, "%T.EffectiveGasTip(...)", tx)
		}

		ltx := &LazyTransaction{
			LazyTransaction: &txpool.LazyTransaction{
				GasFeeCap: feeCap,
				GasTipCap: tipCap,
			},
		}
		got := ltx.effectiveGasTip(baseFee)

		if got.ToBig().Cmp(want) != 0 {
			t.Logf("Fee cap: %v", feeCap)
			t.Logf("Tip cap: %v", tipCap)
			t.Logf("Base fee: %v", baseFee)
			t.Errorf("%T.effectiveGasTip(...) got %v; want %v", ltx, got, want)
		}
	})
}
