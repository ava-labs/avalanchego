// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saexec

import (
	"context"
	"encoding/binary"
	"math"
	"math/big"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm"
	libevmhookstest "github.com/ava-labs/libevm/libevm/hookstest"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/triedb"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/blocks/blockstest"
	"github.com/ava-labs/strevm/cmputils"
	"github.com/ava-labs/strevm/gastime"
	"github.com/ava-labs/strevm/hook"
	saehookstest "github.com/ava-labs/strevm/hook/hookstest"
	"github.com/ava-labs/strevm/proxytime"
	"github.com/ava-labs/strevm/saetest"
	"github.com/ava-labs/strevm/saetest/escrow"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(
		m,
		goleak.IgnoreCurrent(),
		// Despite the call to [snapshot.Tree.Disable] in [Executor.Close], this
		// still leaks at shutdown. This is acceptable as we only ever have one
		// [Executor], which we expect to be running for the entire life of the
		// process.
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core/state/snapshot.(*diskLayer).generate"),
	)
}

// SUT is the system under test, primarily the [Executor].
type SUT struct {
	*Executor
	chain  *blockstest.ChainBuilder
	wallet *saetest.Wallet
	logger *saetest.TBLogger
	db     ethdb.Database
}

// newSUT returns a new SUT. Any >= [logging.Error] on the logger will also
// cancel the returned context, which is useful when waiting for blocks that
// can never finish execution because of an error.
func newSUT(tb testing.TB, hooks *saehookstest.Stub) (context.Context, SUT) {
	tb.Helper()

	logger := saetest.NewTBLogger(tb, logging.Warn)
	ctx := logger.CancelOnError(tb.Context())

	config := saetest.ChainConfig()
	db := rawdb.NewMemoryDatabase()
	tdbConfig := &triedb.Config{}
	xdb := saetest.NewExecutionResultsDB()

	wallet := saetest.NewUNSAFEWallet(tb, 1, types.LatestSigner(config))
	alloc := saetest.MaxAllocFor(wallet.Addresses()...)
	genesis := blockstest.NewGenesis(tb, db, xdb, config, alloc, blockstest.WithTrieDBConfig(tdbConfig), blockstest.WithGasTarget(hooks.Target))

	opts := blockstest.WithBlockOptions(
		blockstest.WithLogger(logger),
	)
	chain := blockstest.NewChainBuilder(config, genesis, opts)
	src := blocks.Source(chain.GetBlock)

	e, err := New(genesis, src.AsHeaderSource(), config, db, xdb, tdbConfig, hooks, logger)
	require.NoError(tb, err, "New()")
	tb.Cleanup(func() {
		require.NoErrorf(tb, e.Close(), "%T.Close()", e)
	})

	return ctx, SUT{
		Executor: e,
		chain:    chain,
		wallet:   wallet,
		logger:   logger,
		db:       db,
	}
}

func defaultHooks() *saehookstest.Stub {
	return &saehookstest.Stub{Target: 1e6}
}

func TestImmediateShutdownNonBlocking(t *testing.T) {
	newSUT(t, defaultHooks()) // calls [Executor.Close] in test cleanup
}

func TestExecutionSynchronisation(t *testing.T) {
	ctx, sut := newSUT(t, defaultHooks())
	e, chain := sut.Executor, sut.chain

	for range 10 {
		b := chain.NewBlock(t, nil)
		require.NoError(t, e.Enqueue(ctx, b), "Enqueue()")
	}

	final := chain.Last()
	require.NoErrorf(t, final.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted() on last-enqueued block", final)
	assert.Equal(t, final.NumberU64(), e.LastExecuted().NumberU64(), "Last-executed atomic pointer holds last-enqueued block")

	for _, b := range chain.AllBlocks() {
		assert.Truef(t, b.Executed(), "%T[%d].Executed()", b, b.NumberU64())
	}
}

func TestReceiptPropagation(t *testing.T) {
	ctx, sut := newSUT(t, defaultHooks())
	e, chain, wallet := sut.Executor, sut.chain, sut.wallet

	var want [][]*types.Receipt
	for range 10 {
		var (
			txs      types.Transactions
			receipts types.Receipts
		)

		for range 5 {
			tx := wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
				Gas:      1e5,
				GasPrice: big.NewInt(1),
			})
			txs = append(txs, tx)
			receipts = append(receipts, &types.Receipt{TxHash: tx.Hash()})
		}
		want = append(want, receipts)

		b := chain.NewBlock(t, txs)
		require.NoError(t, e.Enqueue(ctx, b), "Enqueue()")
	}
	require.NoErrorf(t, chain.Last().WaitUntilExecuted(ctx), "%T.WaitUntilExecuted() on last-enqueued block", chain.Last())

	var got [][]*types.Receipt
	for _, b := range chain.AllExceptGenesis() {
		got = append(got, b.Receipts())
	}
	if diff := cmp.Diff(want, got, cmputils.ReceiptsByTxHash()); diff != "" {
		t.Errorf("%T diff (-want +got):\n%s", got, diff)
	}

	t.Run("RecentReceipt", func(t *testing.T) {
		for _, rs := range want {
			for _, r := range rs {
				t.Run(r.TxHash.String(), func(t *testing.T) {
					// We call the function twice to ensure that the value is
					// returned to the buffered channel, ready for the next one.
					for range 2 {
						got, gotOK, err := sut.RecentReceipt(ctx, r.TxHash)
						require.NoError(t, err)
						assert.True(t, gotOK)
						assert.Equalf(t, r.TxHash, got.TxHash, "%T.TxHash", r)
					}
				})
			}
		}
	})
}

func TestSubscriptions(t *testing.T) {
	ctx, sut := newSUT(t, defaultHooks())
	e, chain, wallet := sut.Executor, sut.chain, sut.wallet

	precompile := common.Address{'p', 'r', 'e'}
	stub := &libevmhookstest.Stub{
		PrecompileOverrides: map[common.Address]libevm.PrecompiledContract{
			precompile: vm.NewStatefulPrecompile(func(env vm.PrecompileEnvironment, input []byte) (ret []byte, err error) {
				env.StateDB().AddLog(&types.Log{
					Address: precompile,
				})
				return nil, nil
			}),
		},
	}
	stub.Register(t)

	gotChainHeadEvents := saetest.NewEventCollector(e.SubscribeChainHeadEvent)
	gotChainEvents := saetest.NewEventCollector(e.SubscribeChainEvent)
	gotLogsEvents := saetest.NewEventCollector(e.SubscribeLogsEvent)
	var (
		wantChainHeadEvents []core.ChainHeadEvent
		wantChainEvents     []core.ChainEvent
		wantLogsEvents      [][]*types.Log
	)

	for range 10 {
		tx := wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       &precompile,
			GasPrice: big.NewInt(1),
			Gas:      1e6,
		})

		b := chain.NewBlock(t, types.Transactions{tx})
		require.NoError(t, e.Enqueue(ctx, b), "Enqueue()")

		wantChainHeadEvents = append(wantChainHeadEvents, core.ChainHeadEvent{
			Block: b.EthBlock(),
		})
		logs := []*types.Log{{
			Address:     precompile,
			BlockNumber: b.NumberU64(),
			TxHash:      tx.Hash(),
			BlockHash:   b.Hash(),
		}}
		wantChainEvents = append(wantChainEvents, core.ChainEvent{
			Block: b.EthBlock(),
			Hash:  b.Hash(),
			Logs:  logs,
		})
		wantLogsEvents = append(wantLogsEvents, logs)
	}

	opt := cmputils.BlocksByHash()
	t.Run("ChainHeadEvents", func(t *testing.T) {
		testEvents(ctx, t, gotChainHeadEvents, wantChainHeadEvents, opt)
	})
	t.Run("ChainEvents", func(t *testing.T) {
		testEvents(ctx, t, gotChainEvents, wantChainEvents, opt)
	})
	t.Run("LogsEvents", func(t *testing.T) {
		testEvents(ctx, t, gotLogsEvents, wantLogsEvents)
	})
}

func testEvents[T any](ctx context.Context, tb testing.TB, got *saetest.EventCollector[T], want []T, opts ...cmp.Option) {
	tb.Helper()
	// There is an invariant that stipulates [blocks.Block.MarkExecuted] MUST
	// occur before sending external events, which means that we can't rely on
	// [blocks.Block.WaitUntilExecuted] to avoid races.
	require.NoErrorf(tb, got.WaitForAtLeast(ctx, len(want)), "%T.WaitForAtLeast()", got)

	require.NoError(tb, got.Unsubscribe())
	if diff := cmp.Diff(want, got.All(), opts...); diff != "" {
		tb.Errorf("Collecting %T from event.Subscription; diff (-want +got):\n%s", want, diff)
	}
}

func TestExecution(t *testing.T) {
	ctx, sut := newSUT(t, defaultHooks())
	wallet := sut.wallet
	eoa := wallet.Addresses()[0]

	var (
		txs  types.Transactions
		want types.Receipts
	)
	deploy := wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		Data:     escrow.CreationCode(),
		GasPrice: big.NewInt(1),
		Gas:      1e7,
	})
	contract := crypto.CreateAddress(eoa, deploy.Nonce())
	txs = append(txs, deploy)
	want = append(want, &types.Receipt{
		TxHash:            deploy.Hash(),
		ContractAddress:   contract,
		EffectiveGasPrice: big.NewInt(1),
	})

	rng := rand.New(rand.NewPCG(0, 0)) //nolint:gosec // Reproducibility is useful for tests
	var wantEscrowBalance uint64
	for range 10 {
		val := rng.Uint64N(100_000)
		tx := wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       &contract,
			Value:    new(big.Int).SetUint64(val),
			GasPrice: big.NewInt(1),
			Gas:      1e6,
			Data:     escrow.CallDataToDeposit(eoa),
		})
		wantEscrowBalance += val
		t.Logf("Depositing %d", val)

		txs = append(txs, tx)
		ev := escrow.DepositEvent(eoa, uint256.NewInt(val))
		ev.Address = contract
		ev.TxHash = tx.Hash()
		want = append(want, &types.Receipt{
			TxHash:            tx.Hash(),
			EffectiveGasPrice: big.NewInt(1),
			Logs:              []*types.Log{ev},
		})
	}

	b := sut.chain.NewBlock(t, txs)

	var logIndex uint
	for i, r := range want {
		ui := uint(i) //nolint:gosec // Known to not overflow

		r.Status = 1
		r.TransactionIndex = ui
		r.BlockHash = b.Hash()
		r.BlockNumber = big.NewInt(1)

		for _, l := range r.Logs {
			l.TxIndex = ui
			l.BlockHash = b.Hash()
			l.BlockNumber = 1
			l.Index = logIndex
			logIndex++
		}
	}

	e := sut.Executor
	require.NoError(t, e.Enqueue(ctx, b), "Enqueue()")
	require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)

	opts := cmp.Options{
		cmpopts.IgnoreFields(
			types.Receipt{},
			"GasUsed", "CumulativeGasUsed",
			"Bloom",
		),
		cmputils.BigInts(),
	}
	if diff := cmp.Diff(want, b.Receipts(), opts); diff != "" {
		t.Errorf("%T.Receipts() diff (-want +got):\n%s", b, diff)
	}

	t.Run("committed_state", func(t *testing.T) {
		sdb, err := state.New(b.PostExecutionStateRoot(), e.StateCache(), nil)
		require.NoErrorf(t, err, "state.New(%T.PostExecutionStateRoot(), %T.StateCache(), nil)", b, e)

		if got, want := sdb.GetBalance(contract).ToBig(), new(big.Int).SetUint64(wantEscrowBalance); got.Cmp(want) != 0 {
			t.Errorf("After Escrow deposits, got contract balance %v; want %v", got, want)
		}

		enablePUSH0 := vm.BlockContext{
			BlockNumber: big.NewInt(1),
			Time:        1,
			Random:      &common.Hash{},
		}
		evm := vm.NewEVM(enablePUSH0, vm.TxContext{}, sdb, e.ChainConfig(), vm.Config{})

		got, _, err := evm.StaticCall(vm.AccountRef(eoa), contract, escrow.CallDataForBalance(eoa), 1e6)
		require.NoErrorf(t, err, "%T.Call([Escrow contract], [balance(eoa)])", evm)
		if got, want := new(uint256.Int).SetBytes(got), uint256.NewInt(wantEscrowBalance); !got.Eq(want) {
			t.Errorf("Escrow.balance([eoa]) got %v; want %v", got, want)
		}
	})
}

func TestEndOfBlockOps(t *testing.T) {
	hooks := defaultHooks()
	ctx, sut := newSUT(t, hooks)
	wallet := sut.wallet
	exportEOA := wallet.Addresses()[0]
	// The import EOA is deliberately not from [newSUT] to ensure that it's got
	// a zero starting balance.
	importEOA := common.Address{'i', 'm', 'p', 'o', 'r', 't'}

	initialTime := sut.lastExecuted.Load().ExecutedByGasTime()

	hooks.Ops = []hook.Op{
		{
			Gas: 100_000,
			Burn: map[common.Address]hook.AccountDebit{
				exportEOA: {
					Amount:     *uint256.NewInt(10),
					MinBalance: *uint256.NewInt(10),
				},
			},
		},
		{
			Gas: 150_000,
			Mint: map[common.Address]uint256.Int{
				importEOA: *uint256.NewInt(100),
			},
		},
	}

	b := sut.chain.NewBlock(t, nil)
	e := sut.Executor
	require.NoError(t, e.Enqueue(ctx, b), "Enqueue()")
	require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)

	t.Run("committed_state", func(t *testing.T) {
		sdb, err := state.New(b.PostExecutionStateRoot(), e.StateCache(), nil)
		require.NoErrorf(t, err, "state.New(%T.PostExecutionStateRoot(), %T.StateCache(), nil)", b, e)
		assert.Equal(t, uint64(1), sdb.GetNonce(exportEOA), "export nonce incremented")
		assert.Zero(t, sdb.GetNonce(importEOA), "import nonce unchanged")
		assert.Equal(t, uint256.NewInt(100), sdb.GetBalance(importEOA), "imported balance")

		wantTime := initialTime.Clone()
		wantTime.Tick(100_000 + 150_000)

		opt := gastime.CmpOpt()
		if diff := cmp.Diff(wantTime, b.ExecutedByGasTime(), opt); diff != "" {
			t.Errorf("%T.ExecutedByGasTime() diff (-want +got):\n%s", b, diff)
		}
	})
}

func TestGasAccounting(t *testing.T) {
	const gasPerTx = gas.Gas(params.TxGas)
	hooks := &saehookstest.Stub{
		Target: 5 * gasPerTx,
	}
	ctx, sut := newSUT(t, hooks)

	at := func(blockTime, txs uint64, rate gas.Gas) *proxytime.Time[gas.Gas] {
		tm := proxytime.New[gas.Gas](blockTime, rate)
		tm.Tick(gas.Gas(txs) * gasPerTx)
		return tm
	}

	// If this fails then all of the tests need to be adjusted. This is cleaner
	// than polluting the test cases with a repetitive identifier.
	require.Equal(t, 2, gastime.TargetToRate, "gastime.TargetToRate assumption")

	// Steps are _not_ independent, so the execution time of one is the starting
	// time of the next.
	steps := []struct {
		blockTime      uint64
		numTxs         int
		gasTipCap      uint64
		targetAfter    gas.Gas
		wantExecutedBy *proxytime.Time[gas.Gas]
		// Because of the 2:1 ratio between Rate and Target, gas consumption
		// increases excess by half of the amount consumed, while
		// fast-forwarding reduces excess by half of the amount skipped.
		wantExcessAfter gas.Gas
		wantPriceAfter  gas.Price
	}{
		{
			blockTime:       2,
			numTxs:          3,
			targetAfter:     5 * gasPerTx,
			wantExecutedBy:  at(2, 3, 10*gasPerTx),
			wantExcessAfter: 3 * gasPerTx / 2,
			wantPriceAfter:  1, // Excess isn't high enough so price is effectively e^0
		},
		{
			blockTime:       3, // fast-forward
			numTxs:          12,
			targetAfter:     5 * gasPerTx,
			wantExecutedBy:  at(4, 2, 10*gasPerTx),
			wantExcessAfter: 12 * gasPerTx / 2,
			wantPriceAfter:  1,
		},
		{
			blockTime:       4, // no fast-forward so starts at last execution time
			numTxs:          20,
			targetAfter:     5 * gasPerTx,
			wantExecutedBy:  at(6, 2, 10*gasPerTx),
			wantExcessAfter: (12 + 20) * gasPerTx / 2,
			wantPriceAfter:  1,
		},
		{
			blockTime:   7, // fast-forward equivalent of 8 txs
			numTxs:      16,
			targetAfter: 10 * gasPerTx, // double gas/block --> halve ticking rate
			// Doubling the target scales both the ending time and excess to compensate.
			wantExecutedBy:  at(8, 2*6, 2*10*gasPerTx),
			wantExcessAfter: 2 * (12 + 20 - 8 + 16) * gasPerTx / 2,
			wantPriceAfter:  1,
		},
		{
			blockTime:   8, // no fast-forward
			numTxs:      4,
			targetAfter: 5 * gasPerTx, // back to original
			// Halving the target inverts the scaling seen in the last block.
			wantExecutedBy:  at(8, 6+(4/2), 10*gasPerTx),
			wantExcessAfter: ((12 + 20 - 8 + 16) + 4/2) * gasPerTx / 2,
			wantPriceAfter:  1,
		},
		{
			blockTime:       8,
			numTxs:          5,
			targetAfter:     5 * gasPerTx,
			wantExecutedBy:  at(8, 6+(4/2)+5, 10*gasPerTx),
			wantExcessAfter: ((12 + 20 - 8 + 16) + 4/2 + 5) * gasPerTx / 2,
			wantPriceAfter:  1,
		},
		{
			blockTime:       20, // more than double the last executed-by time, reduces excess to 0
			numTxs:          1,
			targetAfter:     5 * gasPerTx,
			wantExecutedBy:  at(20, 1, 10*gasPerTx),
			wantExcessAfter: gasPerTx / 2,
			wantPriceAfter:  1,
		},
		{
			blockTime:       21,                                 // fast-forward so excess is 0
			numTxs:          30 * gastime.TargetToExcessScaling, // deliberate, see below
			targetAfter:     5 * gasPerTx,
			wantExecutedBy:  at(21, 30*gastime.TargetToExcessScaling, 10*gasPerTx),
			wantExcessAfter: 3 * ((5 * gasPerTx /*T*/) * gastime.TargetToExcessScaling /* == K */),
			// Excess is now 3·K so the price is e^3
			wantPriceAfter: gas.Price(math.Floor(math.Exp(3 /* <----- NB */))),
		},
		{
			blockTime:       22, // no fast-forward
			numTxs:          10 * gastime.TargetToExcessScaling,
			gasTipCap:       1, // anything non-zero, to exercise [types.Receipt.EffectiveGasPrice]
			targetAfter:     5 * gasPerTx,
			wantExecutedBy:  at(21, 40*gastime.TargetToExcessScaling, 10*gasPerTx),
			wantExcessAfter: 4 * ((5 * gasPerTx /*T*/) * gastime.TargetToExcessScaling /* == K */),
			wantPriceAfter:  gas.Price(math.Floor(math.Exp(4 /* <----- NB */))),
		},
	}

	e, chain, wallet := sut.Executor, sut.chain, sut.wallet

	var maxGasFeeCap uint64
	price := gas.Price(1)
	for _, s := range steps {
		maxGasFeeCap = max(maxGasFeeCap, uint64(price)+s.gasTipCap)
		price = s.wantPriceAfter
	}

	for i, step := range steps {
		hooks.Target = step.targetAfter

		txs := make(types.Transactions, step.numTxs)
		for i := range txs {
			txs[i] = wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
				To:        &common.Address{},
				Gas:       params.TxGas,
				GasTipCap: uint256.NewInt(step.gasTipCap).ToBig(),
				GasFeeCap: uint256.NewInt(maxGasFeeCap).ToBig(),
			})
		}

		b := chain.NewBlock(t, txs, blockstest.WithEthBlockOptions(
			blockstest.ModifyHeader(func(h *types.Header) {
				h.Time = step.blockTime
			}),
		))
		require.NoError(t, e.Enqueue(ctx, b), "Enqueue()")
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)

		opt := proxytime.CmpOpt[gas.Gas](proxytime.IgnoreRateInvariants)
		if diff := cmp.Diff(step.wantExecutedBy, b.ExecutedByGasTime().Time, opt); diff != "" {
			t.Errorf("%T.ExecutedByGasTime().Time diff (-want +got):\n%s", b, diff)
		}

		t.Run("CumulativeGasUsed", func(t *testing.T) {
			for i, r := range b.Receipts() {
				ui := uint64(i + 1) //nolint:gosec // Known to not overflow
				assert.Equalf(t, ui*params.TxGas, r.CumulativeGasUsed, "%T.Receipts()[%d]", b, i)
			}
		})

		if t.Failed() {
			// Future steps / tests may be corrupted and false-positive errors
			// aren't helpful.
			break
		}

		t.Run("gas_price", func(t *testing.T) {
			tm := b.ExecutedByGasTime().Clone()
			assert.Equalf(t, step.wantExcessAfter, tm.Excess(), "%T.Excess()", tm)
			assert.Equalf(t, step.wantPriceAfter, tm.Price(), "%T.Price()", tm)

			wantBaseFee := gas.Price(1)
			if i > 0 {
				wantBaseFee = steps[i-1].wantPriceAfter
			}
			require.Truef(t, b.BaseFee().IsUint64(), "%T.BaseFee().IsUint64()", b)
			assert.Equalf(t, wantBaseFee, gas.Price(b.BaseFee().Uint64()), "%T.BaseFee().Uint64()", b)

			t.Run("EffectiveGasPrice", func(t *testing.T) {
				want := uint256.NewInt(uint64(wantBaseFee) + step.gasTipCap)
				for i, r := range b.Receipts() {
					if got := r.EffectiveGasPrice; got.Cmp(want.ToBig()) != 0 {
						t.Errorf("%T.Receipts()[%d].EffectiveGasPrice = %v; want %v", b, i, got, want)
					}
				}
			})
		})
	}
	if t.Failed() {
		t.Fatal("Chain in unexpected state")
	}

	t.Run("BASEFEE_op_code", func(t *testing.T) {
		finalPrice := uint64(steps[len(steps)-1].wantPriceAfter)

		tx := wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       nil, // runs call data as a constructor
			Gas:      100e6,
			GasPrice: new(big.Int).SetUint64(finalPrice),
			Data:     asBytes(logTopOfStackAfter(vm.BASEFEE)...),
		})

		b := chain.NewBlock(t, types.Transactions{tx})
		require.NoError(t, e.Enqueue(ctx, b), "Enqueue()")
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
		require.Lenf(t, b.Receipts(), 1, "%T.Receipts()", b)
		require.Lenf(t, b.Receipts()[0].Logs, 1, "%T.Receipts()[0].Logs", b)

		got := b.Receipts()[0].Logs[0].Topics[0]
		want := common.BytesToHash(binary.BigEndian.AppendUint64(nil, finalPrice))
		assert.Equal(t, want, got)
	})
}

// logTopOfStackAfter returns contract bytecode that logs the value on the top
// of the stack after executing `pre`.
func logTopOfStackAfter(pre ...vm.OpCode) []vm.OpCode {
	return slices.Concat(pre, []vm.OpCode{vm.PUSH0, vm.PUSH0, vm.LOG1})
}

func asBytes(ops ...vm.OpCode) []byte {
	buf := make([]byte, len(ops))
	for i, op := range ops {
		buf[i] = byte(op)
	}
	return buf
}

func FuzzOpCodes(f *testing.F) {
	// Although it's tempting to run multiple `code` slices in a block, to
	// amortise the fixed setup cost of the SUT, this stops the Go fuzzer from
	// knowing about their independence, resulting in a lot of empty inputs.
	f.Fuzz(func(t *testing.T, code []byte) {
		t.Parallel() // for corpus in ./testdata/

		_, sut := newSUT(t, defaultHooks())
		tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       nil, // i.e. contract creation, resulting in `code` being executed
			GasPrice: big.NewInt(1),
			Gas:      30e6,
			Data:     code,
		})
		b := sut.chain.NewBlock(t, types.Transactions{tx})

		// Ensure that the SUT [logging.Logger] remains of this type so >=WARN
		// logs become failures.
		//nolint:staticcheck
		var logger *saetest.TBLogger = sut.logger
		// Errors in execution (i.e. reverts) are fine, but we don't want them
		// bubbling up any further.
		require.NoErrorf(t, sut.execute(b, logger), "%T.execute()", sut.Executor)
	})
}

func TestContextualOpCodes(t *testing.T) {
	ctx, sut := newSUT(t, defaultHooks())

	chain := sut.chain
	for range 5 {
		// Historical blocks, required to already be in `chain`, for testing
		// BLOCKHASH.
		b := chain.NewBlock(t, nil)
		require.NoErrorf(t, sut.Enqueue(ctx, b), "Enqueue([empty block])")
	}

	bigToHash := func(b *big.Int) common.Hash {
		return uint256.MustFromBig(b).Bytes32()
	}

	// For specific tests.
	const txValueSend = 42
	saveBlockNum := &blockNumSaver{}

	tests := []struct {
		name        string
		code        []vm.OpCode
		header      func(*types.Header)
		wantTopic   common.Hash
		wantTopicFn func() common.Hash // if non-nil, overrides `wantTopic`
	}{
		{
			name:      "BALANCE_of_ADDRESS",
			code:      logTopOfStackAfter(vm.ADDRESS, vm.BALANCE),
			wantTopic: common.Hash{31: txValueSend},
		},
		{
			name:      "CALLVALUE",
			code:      logTopOfStackAfter(vm.CALLVALUE),
			wantTopic: common.Hash{31: txValueSend},
		},
		{
			name:      "SELFBALANCE",
			code:      logTopOfStackAfter(vm.SELFBALANCE),
			wantTopic: common.Hash{31: txValueSend},
		},
		{
			name: "ORIGIN",
			code: logTopOfStackAfter(vm.ORIGIN),
			wantTopic: common.BytesToHash(
				sut.wallet.Addresses()[0].Bytes(),
			),
		},
		{
			name: "CALLER",
			code: logTopOfStackAfter(vm.CALLER),
			wantTopic: common.BytesToHash(
				sut.wallet.Addresses()[0].Bytes(),
			),
		},
		{
			name:      "BLOCKHASH_genesis",
			code:      logTopOfStackAfter(vm.PUSH0, vm.BLOCKHASH),
			wantTopic: chain.AllBlocks()[0].Hash(),
		},
		{
			name:      "BLOCKHASH_arbitrary",
			code:      logTopOfStackAfter(vm.PUSH1, 3, vm.BLOCKHASH),
			wantTopic: chain.AllBlocks()[3].Hash(),
		},
		{
			name:   "NUMBER",
			code:   logTopOfStackAfter(vm.NUMBER),
			header: saveBlockNum.store,
			wantTopicFn: func() common.Hash {
				return bigToHash(saveBlockNum.num)
			},
		},
		{
			name: "COINBASE_arbitrary",
			code: logTopOfStackAfter(vm.COINBASE),
			header: func(h *types.Header) {
				h.Coinbase = common.Address{17: 0xC0, 18: 0xFF, 19: 0xEE}
			},
			wantTopic: common.BytesToHash([]byte{0xC0, 0xFF, 0xEE}),
		},
		{
			name: "COINBASE_zero",
			code: logTopOfStackAfter(vm.COINBASE),
		},
		{
			name: "TIMESTAMP",
			code: logTopOfStackAfter(vm.TIMESTAMP),
			header: func(h *types.Header) {
				h.Time = 0xDECAFBAD
			},
			wantTopic: common.BytesToHash([]byte{0xDE, 0xCA, 0xFB, 0xAD}),
		},
		{
			name: "PREVRANDAO",
			code: logTopOfStackAfter(vm.PREVRANDAO),
		},
		{
			name: "GASLIMIT",
			code: logTopOfStackAfter(vm.GASLIMIT),
			header: func(h *types.Header) {
				h.GasLimit = 0xA11CEB0B
			},
			wantTopic: common.BytesToHash([]byte{0xA1, 0x1C, 0xEB, 0x0B}),
		},
		{
			name:      "CHAINID",
			code:      logTopOfStackAfter(vm.CHAINID),
			wantTopic: bigToHash(sut.ChainConfig().ChainID),
		},
		{
			name: "BLOBBASEFEE",
			code: logTopOfStackAfter(vm.BLOBBASEFEE),
			header: func(h *types.Header) {
				h.ExcessBlobGas = new(uint64)
			},
			wantTopic: common.Hash{31: 1},
		},
		// BASEFEE is tested in [TestGasAccounting] because getting the clock
		// excess to a specific value is complicated.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
				To:       nil, // contract creation runs the call data (one sneaky trick blockchain developers don't want you to know)
				GasPrice: big.NewInt(1),
				Gas:      100e6,
				Data:     asBytes(tt.code...),
				Value:    big.NewInt(txValueSend),
			})

			var opts []blockstest.ChainOption
			if tt.header != nil {
				opts = append(opts, blockstest.WithEthBlockOptions(
					blockstest.ModifyHeader(tt.header),
				))
			}

			b := sut.chain.NewBlock(t, types.Transactions{tx}, opts...)
			require.NoError(t, sut.Enqueue(ctx, b), "Enqueue()")
			require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
			require.Lenf(t, b.Receipts(), 1, "%T.Receipts()", b)

			got := b.Receipts()[0]
			diffopts := cmp.Options{
				cmpopts.IgnoreFields(
					types.Receipt{},
					"Bloom", "ContractAddress", "CumulativeGasUsed", "GasUsed",
				),
				cmpopts.IgnoreFields(
					types.Log{},
					"Address",
				),
				cmputils.BigInts(),
			}
			wantTopic := tt.wantTopic
			if tt.wantTopicFn != nil {
				wantTopic = tt.wantTopicFn()
			}
			want := &types.Receipt{
				Status:            types.ReceiptStatusSuccessful,
				BlockHash:         b.Hash(),
				BlockNumber:       b.Number(),
				TxHash:            tx.Hash(),
				EffectiveGasPrice: big.NewInt(1),
				Logs: []*types.Log{{
					Topics:      []common.Hash{wantTopic},
					BlockHash:   b.Hash(),
					BlockNumber: b.NumberU64(),
					TxHash:      tx.Hash(),
				}},
			}
			if diff := cmp.Diff(want, got, diffopts); diff != "" {
				t.Errorf("%T diff (-want +got):\n%s", got, diff)
			}
		})
	}
}

type blockNumSaver struct {
	num *big.Int
}

var _ = blockstest.ModifyHeader((*blockNumSaver)(nil).store)

func (e *blockNumSaver) store(h *types.Header) {
	e.num = new(big.Int).Set(h.Number)
}

func TestSnapshotPersistence(t *testing.T) {
	ctx, sut := newSUT(t, defaultHooks())

	e, chain, wallet := sut.Executor, sut.chain, sut.wallet

	const n = 10
	for range n {
		b := chain.NewBlock(t, types.Transactions{
			wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
				To:       &common.Address{},
				Gas:      params.TxGas,
				GasPrice: big.NewInt(1),
			}),
		})
		require.NoError(t, e.Enqueue(ctx, b), "Enqueue()")
	}
	last := chain.Last()
	require.NoErrorf(t, last.WaitUntilExecuted(ctx), "%T.Last().WaitUntilExecuted()", chain)

	require.NoErrorf(t, e.Close(), "%T.Close()", e)
	// [newSUT] creates a cleanup that also calls [Executor.Close], which isn't
	// valid usage. The simplest workaround is to just replace the quit channel
	// so it can be closed again.
	e.quit = make(chan struct{})

	// The crux of the test is whether we can recover the EOA nonce using only a
	// new set of snapshots, recovered from the databases.
	conf := snapshot.Config{
		CacheSize: SnapshotCacheSizeMB,
		NoBuild:   true, // i.e. MUST be loaded from disk
	}
	snaps, err := snapshot.New(conf, sut.db, e.StateCache().TrieDB(), last.PostExecutionStateRoot())
	require.NoError(t, err, "snapshot.New(..., [post-execution state root of last-executed block])")
	snap := snaps.Snapshot(last.PostExecutionStateRoot())
	require.NotNilf(t, snap, "%T.Snapshot([post-execution state root of last-executed block])", snaps)

	t.Run("snap.Account(EOA)", func(t *testing.T) {
		eoa := wallet.Addresses()[0]
		got, err := snap.Account(crypto.Keccak256Hash(eoa.Bytes()))
		require.NoError(t, err)
		require.NotNil(t, got) // yes, this is still possible with nil error
		require.Equalf(t, uint64(n), got.Nonce, "%T.Nonce", got)
	})
}
