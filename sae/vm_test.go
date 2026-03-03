// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"math/big"
	"math/rand/v2"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm"
	libevmhookstest "github.com/ava-labs/libevm/libevm/hookstest"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/strevm/adaptor"
	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/blocks/blockstest"
	"github.com/ava-labs/strevm/cmputils"
	"github.com/ava-labs/strevm/hook/hookstest"
	saeparams "github.com/ava-labs/strevm/params"
	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saetest"
)

func TestMain(m *testing.M) {
	createWorstCaseFuzzFlags(flag.CommandLine)
	flag.Parse()

	log.SetDefault(log.NewLogger(log.NewTerminalHandlerWithLevel(os.Stderr, log.LevelError, true)))

	goleak.VerifyTestMain(
		m,
		goleak.IgnoreCurrent(),
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core/state/snapshot.(*diskLayer).generate"),
		// TxPool.Close() doesn't wait for its loop() method to signal termination.
		goleak.IgnoreTopFunction("github.com/ava-labs/libevm/core/txpool.(*TxPool).loop.func2"),
	)
}

// SUT is the system under test. Testing SHOULD be performed via the embedded
// types as these most accurately reflect the public API. Any access to the
// other fields SHOULD instead be exposed as methods, such as [SUT.stateAt], to
// avoid over-reliance on internal implementation details.
type SUT struct {
	block.ChainVM
	*ethclient.Client
	rpcClient *rpc.Client

	rawVM   *VM
	genesis *blocks.Block
	wallet  *saetest.Wallet
	avaDB   database.Database
	db      ethdb.Database
	hooks   *hookstest.Stub
	logger  *saetest.TBLogger

	validators *validatorstest.State
	sender     *enginetest.Sender
}

type (
	sutConfig struct {
		hooks    *hookstest.Stub
		vmConfig Config
		logLevel logging.Level
		genesis  core.Genesis
		db       database.Database
	}
	sutOption = options.Option[sutConfig]
)

// chainID is made a global to keep it constant across multiple SUTs.
var chainID = ids.GenerateTestID()

func newSUT(tb testing.TB, numAccounts uint, opts ...sutOption) (context.Context, *SUT) {
	tb.Helper()

	mempoolConf := legacypool.DefaultConfig // copies
	mempoolConf.Journal = "/dev/null"

	keys := saetest.NewUNSAFEKeyChain(tb, numAccounts)

	xdb := saetest.NewExecutionResultsDB()
	conf := options.ApplyTo(&sutConfig{
		hooks: &hookstest.Stub{
			Target: 100e6,
			ExecutionResultsDBFn: func(string) (saedb.ExecutionResults, error) {
				return xdb, nil
			},
		},
		vmConfig: Config{
			MempoolConfig: mempoolConf,
		},
		logLevel: logging.Debug,
		genesis: core.Genesis{
			Config:     saetest.ChainConfig(),
			Alloc:      saetest.MaxAllocFor(keys.Addresses()...),
			Timestamp:  saeparams.TauSeconds,
			Difficulty: big.NewInt(0), // irrelevant but required
		},
		db: memdb.New(),
	}, opts...)

	vm := NewSinceGenesis(conf.hooks, conf.vmConfig)
	snow := adaptor.Convert(vm)
	tb.Cleanup(func() {
		ctx := context.WithoutCancel(tb.Context())
		require.NoError(tb, vm.last.accepted.Load().WaitUntilExecuted(ctx), "{last-accepted block}.WaitUntilExecuted()")
		require.NoError(tb, snow.Shutdown(ctx), "Shutdown()")
	})

	logger := saetest.NewTBLogger(tb, conf.logLevel)
	ctx := logger.CancelOnError(tb.Context())
	snowCtx := snowtest.Context(tb, chainID)
	snowCtx.Log = logger

	sender := &enginetest.Sender{
		SendAppGossipF: func(context.Context, snowcommon.SendConfig, []byte) error {
			return nil
		},
	}

	require.NoError(tb, snow.Initialize(
		ctx,
		snowCtx,
		conf.db,
		marshalJSON(tb, conf.genesis),
		nil, // upgrade bytes
		nil, // config bytes (not ChainConfig)
		nil, // Fxs
		sender,
	), "Initialize()")

	rpcClient, ethClient := dialRPC(ctx, tb, snow)

	validators, ok := snowCtx.ValidatorState.(*validatorstest.State)
	require.Truef(tb, ok, "unexpected type %T for snowCtx.ValidatorState", snowCtx.ValidatorState)
	return ctx, &SUT{
		ChainVM:   snow,
		Client:    ethClient,
		rpcClient: rpcClient,
		rawVM:     vm.VM,
		genesis:   vm.last.settled.Load(),
		wallet: saetest.NewWalletWithKeyChain(
			keys,
			types.LatestSigner(conf.genesis.Config),
		),
		avaDB:  conf.db,
		db:     newEthDB(conf.db),
		hooks:  conf.hooks,
		logger: logger,

		validators: validators,
		sender:     sender,
	}
}

func dialRPC(ctx context.Context, tb testing.TB, snow block.ChainVM) (*rpc.Client, *ethclient.Client) {
	tb.Helper()

	handlers, err := snow.CreateHandlers(ctx)
	require.NoErrorf(tb, err, "%T.CreateHandlers()", snow)
	server := httptest.NewServer(handlers[wsHTTPExtensionPath])
	tb.Cleanup(server.Close)
	rpcClient, err := rpc.Dial("ws://" + server.Listener.Addr().String())
	require.NoErrorf(tb, err, "rpc.Dial(http.NewServer(%T.CreateHandlers()))", snow)
	client := ethclient.NewClient(rpcClient)
	tb.Cleanup(client.Close)

	return rpcClient, client
}

func marshalJSON(tb testing.TB, v any) []byte {
	tb.Helper()
	buf, err := json.Marshal(v)
	require.NoErrorf(tb, err, "json.Marshal(%T)", v)
	return buf
}

// CallContext propagates its arguments to and from [SUT.rpcClient.CallContext].
// Embedding both the [ethclient.Client] and the underlying [rpc.Client] isn't
// possible due to a name conflict, so this method is manually exposed.
func (s *SUT) CallContext(ctx context.Context, result any, method string, args ...any) error {
	return s.rpcClient.CallContext(ctx, result, method, args...)
}

type vmTime struct {
	time.Time
}

func (t *vmTime) now() time.Time {
	return t.Time
}

func (t *vmTime) set(n time.Time) {
	t.Time = n
}

func (t *vmTime) advance(d time.Duration) {
	t.Time = t.Time.Add(d)
}

// advanceToSettle advances the time such that the next call to [vmTime.now] is
// at or after the time required to settle `b`. Note that at least one more
// accepted [blocks.Block] is still required to actually settle `b`.
func (t *vmTime) advanceToSettle(ctx context.Context, tb testing.TB, b *blocks.Block) {
	tb.Helper()
	require.NoErrorf(tb, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	to := b.ExecutedByGasTime().AsTime().Add(saeparams.Tau)
	if t.Before(to) {
		t.set(to)
	}
}

// withVMTime returns an option to configure a new SUT's "now" function along
// with a struct to access and set the time at nanosecond resolution.
func withVMTime(tb testing.TB, startTime time.Time) (sutOption, *vmTime) {
	tb.Helper()
	t := &vmTime{
		Time: startTime,
	}
	opt := options.Func[sutConfig](func(c *sutConfig) {
		// TODO(StephenButtolph) unify the time functions provided in the config
		// and the hooks.
		c.hooks.Now = t.now
		c.vmConfig.Now = t.now
	})

	return opt, t
}

// withExecResultsDB returns an option that replaces the default
// execution-results database with the provided one.
func withExecResultsDB(hdb database.HeightIndex) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.hooks.ExecutionResultsDBFn = func(string) (saedb.ExecutionResults, error) {
			return saedb.ExecutionResults{HeightIndex: hdb}, nil
		}
	})
}

func withBloomSectionSize(size uint64) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.RPCConfig.BlocksPerBloomSection = size
	})
}

func (s *SUT) nodeID() ids.NodeID {
	return s.rawVM.snowCtx.NodeID
}

// context returns a [context.Context], derived from the [testing.TB], that is
// cancelled if the SUT's default logger receives a log at [logging.Error] or
// higher.
//
//nolint:thelper // Not a helper
func (s *SUT) context(tb testing.TB) context.Context {
	return s.logger.CancelOnError(tb.Context())
}

func (s *SUT) mustSendTx(tb testing.TB, tx *types.Transaction) {
	tb.Helper()
	require.NoErrorf(tb, s.Client.SendTransaction(s.context(tb), tx), "%T.SendTransaction([%#x])", s.Client, tx.Hash())
}

// addToMempool is a convenience wrapper around [SUT.mustSendTx] (per tx) and
// [SUT.requireInMempool].
func (s *SUT) addToMempool(tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()
	txHashes := make([]common.Hash, len(txs))
	for i, tx := range txs {
		s.mustSendTx(tb, tx)
		txHashes[i] = tx.Hash()
	}
	s.requireInMempool(tb, txHashes...)
}

// syncMempool is a convenience wrapper for calling [txpool.TxPool.Sync], which
// MUST NOT be done in production.
func (s *SUT) syncMempool(tb testing.TB) {
	tb.Helper()
	var _ txpool.TxPool // maintain import for [comment] rendering
	p := s.rawVM.mempool.Pool
	require.NoErrorf(tb, p.Sync(), "%T.Sync()", p)
}

// requireInMempool requires that the transaction with the specified hash is
// eventually in the mempool. It calls [SUT.syncMempool] before every check.
func (s *SUT) requireInMempool(tb testing.TB, txs ...common.Hash) {
	tb.Helper()
	require.EventuallyWithTf(
		tb,
		func(c *assert.CollectT) {
			s.syncMempool(tb)
			for i, tx := range txs {
				assert.Truef(c, s.rawVM.mempool.Pool.Has(tx), "tx %d:%v not in mempool", i, tx)
			}
		},
		250*time.Millisecond, 25*time.Millisecond,
		"all of txs [%v] to in mempool", txs,
	)
}

// buildAndParseBlock calls [SUT.addToMempool] with the provided transactions,
// builds a new block and passes its bytes to [VM.ParseBlock], the result of
// which is returned. This is equivalent to a validator having received valid
// bytes from a peer, but with no element of consensus performed.
//
// A block at this stage is rarely useful and does not meet invariants required
// of a [blocks.Block], hence its return as a [snowman.Block].
func (s *SUT) buildAndParseBlock(tb testing.TB, preference *blocks.Block, txs ...*types.Transaction) snowman.Block {
	tb.Helper()
	s.addToMempool(tb, txs...)

	ctx := s.context(tb)
	require.NoError(tb, s.SetPreference(ctx, preference.ID()), "SetPreference()")

	proposed, err := s.BuildBlock(ctx)
	require.NoError(tb, err, "BuildBlock()")
	b, err := s.ParseBlock(ctx, proposed.Bytes())
	require.NoError(tb, err, "ParseBlock(BuildBlock().Bytes())")
	return b
}

// createAndVerifyBlock calls [SUT.buildAndParseBlock] with the provided
// transactions. It verifies the block with [VM.VerifyBlock] (via the [adaptor])
// before returning it.
//
// Although the block is now in a functional state, it is still returned as a
// [snowman.Block] to expose its `Accept()` method, ensuring that we test via
// the public API as exposed to the consensus engine.
func (s *SUT) createAndVerifyBlock(tb testing.TB, preference *blocks.Block, txs ...*types.Transaction) snowman.Block {
	tb.Helper()
	b := s.buildAndParseBlock(tb, preference, txs...)
	require.NoErrorf(tb, b.Verify(s.context(tb)), "%T.Verify()", b)
	return b
}

// runConsensusLoopOnPreference is equivalent to [SUT.createAndVerifyBlock]
// except that it also accepts the block with [VM.AcceptBlock]. It does NOT wait
// for it to be executed; to do this automatically, set the [VM] to
// [snow.Bootstrapping].
//
// There is no longer any need to wrap the block as an [adaptor.Block] so it is
// returned in its raw form, unlike earlier steps in the consenus loop.
func (s *SUT) runConsensusLoopOnPreference(tb testing.TB, preference *blocks.Block, txs ...*types.Transaction) *blocks.Block {
	tb.Helper()
	b := s.createAndVerifyBlock(tb, preference, txs...)
	require.NoErrorf(tb, b.Accept(s.context(tb)), "%T.Accept()", b)
	return unwrap(tb, b)
}

// runConsensusLoop is a convenience wrapper for
// [SUT.runConsensusLoopOnPreference], using [SUT.lastAcceptedBlock] as the
// preference.
func (s *SUT) runConsensusLoop(tb testing.TB, txs ...*types.Transaction) *blocks.Block {
	tb.Helper()
	return s.runConsensusLoopOnPreference(tb, s.lastAcceptedBlock(tb), txs...)
}

// waitUntilExecuted blocks until an external indicator shows that `b` has been
// executed.
func (s *SUT) waitUntilExecuted(tb testing.TB, b *blocks.Block) {
	tb.Helper()
	defer func() {
		tb.Helper()
		require.True(tb, b.Executed(), "%T.Executed()", b)
	}()

	// The subscription is opened before checking the block number to avoid
	// missing the notification that the block was executed.
	c := make(chan *types.Header)
	ctx := tb.Context()
	sub, err := s.SubscribeNewHead(ctx, c)
	require.NoErrorf(tb, err, "%T.SubscribeNewHead()", s.Client)
	defer sub.Unsubscribe()

	num, err := s.BlockNumber(ctx)
	require.NoErrorf(tb, err, "%T.BlockNumber()", s.Client)
	if num >= b.Height() {
		return
	}

	for {
		select {
		case <-ctx.Done():
			tb.Fatalf("waiting for block %d to execute: %v", b.Height(), ctx.Err())
		case err := <-sub.Err():
			tb.Fatalf("%T.SubscribeNewHead().Err() returned: %v", s.Client, err)
		case h := <-c:
			if h.Number.Uint64() >= b.Height() {
				return
			}
		}
	}
}

func (s *SUT) stateAt(tb testing.TB, root common.Hash) *state.StateDB {
	tb.Helper()
	sdb, err := state.New(root, s.rawVM.exec.StateCache(), nil)
	require.NoErrorf(tb, err, "state.New(%#x, %T.StateCache())", root, s.rawVM.exec)
	return sdb
}

// lastAcceptedBlock is a convenience wrapper for calling [VM.GetBlock] with
// the ID from [VM.LastAccepted] as an argument.
func (s *SUT) lastAcceptedBlock(tb testing.TB) *blocks.Block {
	tb.Helper()
	ctx := s.context(tb)
	id, err := s.LastAccepted(ctx)
	require.NoError(tb, err, "LastAccepted()")
	b, err := s.GetBlock(ctx, id)
	require.NoError(tb, err, "GetBlock(lastAcceptedID)")
	return unwrap(tb, b)
}

// unwrap is a convenience (un)wrapper for calling [adaptor.Block.Unwrap] after
// confirming the concrete type of `b`.
func unwrap(tb testing.TB, b snowman.Block) *blocks.Block {
	tb.Helper()
	switch b := b.(type) {
	case adaptor.Block[*blocks.Block]:
		return b.Unwrap()
	default:
		tb.Fatalf("snowman.Block of concrete type %T", b)
		return nil
	}
}

// assertBlockHashInvariants MUST NOT be called concurrently with
// [VM.AcceptBlock] as it depends on the last-accepted block. It also blocks
// until said block has finished execution.
func (s *SUT) assertBlockHashInvariants(ctx context.Context, t *testing.T) {
	t.Helper()
	t.Run("block_hash_invariants", func(t *testing.T) {
		b := s.lastAcceptedBlock(t)
		// The API client is an external reader, so we must wait on an external
		// indicator. The block's WaitUntilExecuted is only an internal
		// indicator.
		s.waitUntilExecuted(t, b)
		t.Logf("Last accepted (and executed) block: %d", b.Height())

		for num, want := range map[rpc.BlockNumber]common.Hash{
			rpc.BlockNumber(b.Number().Int64()): b.Hash(),
			rpc.LatestBlockNumber:               b.Hash(),               // Because we've waited until it's executed
			rpc.SafeBlockNumber:                 b.LastSettled().Hash(), // Safe from disk corruption, not re-org, as acceptance guarantees finality
			rpc.FinalizedBlockNumber:            b.LastSettled().Hash(), // Because we maintain label monotonicity
		} {
			t.Run(num.String(), func(t *testing.T) {
				got, err := s.Client.HeaderByNumber(ctx, big.NewInt(num.Int64()))
				require.NoErrorf(t, err, "%T.HeaderByNumber(%v)", s.Client, num)
				assert.Equalf(t, want, got.Hash(), "%T.HeaderByNumber(%v).Hash()", s.Client, num)
			})
		}

		// The RPC implementation doesn't use the database to resolve the block
		// labels above, so we still need to check them.
		assert.Equal(t, b.Hash(), rawdb.ReadHeadBlockHash(s.db), "rawdb.ReadHeadBlockHash() MUST reflect last-executed block")
		assert.Equal(t, b.LastSettled().Hash(), rawdb.ReadFinalizedBlockHash(s.db), "rawdb.ReadFinalizedBlockHash() MUST reflect last-settled block")
	})
}

func TestIntegration(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	testWaitForEvent := func(t *testing.T) {
		t.Helper()
		ev, err := sut.WaitForEvent(ctx)
		require.NoError(t, err)
		require.Equal(t, snowcommon.PendingTxs, ev)
	}

	waitForEvDone := make(chan struct{})
	go func() {
		defer close(waitForEvDone)
		t.Run("WaitForEvent_early_unblocks", testWaitForEvent)
	}()

	transfer := uint256.NewInt(42)
	recipient := common.Address{1, 2, 3, 4}
	const numTxs = 2
	for range numTxs {
		tx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &recipient,
			Gas:       params.TxGas,
			GasFeeCap: big.NewInt(1),
			Value:     transfer.ToBig(),
		})
		sut.mustSendTx(t, tx)
	}

	select {
	case <-waitForEvDone:
	case <-time.After(time.Second):
		t.Error("WaitForEvent() called before SendTx() did not unblock")
	}

	// Each tx sent to the VM can unblock at most 1 call to [VM.WaitForEvent] so
	// making an extra call proves that it returns due to pending txs already in
	// the mempool. Yes, the one above would be a sufficient extra, but it's
	// best to keep this logic self-contained, should the unblocking test be
	// removed.
	for range numTxs + 1 {
		t.Run("WaitForEvent_with_existing_txs", testWaitForEvent)
	}

	sut.syncMempool(t) // technically we've only proven 1 tx added, as unlikely as a race is
	require.Equal(t, numTxs, sut.rawVM.numPendingTxs(), "number of pending txs")

	b := sut.runConsensusLoopOnPreference(t, sut.genesis)
	assert.Equal(t, sut.genesis.ID(), b.Parent(), "BuildBlock() builds on preference")
	require.Lenf(t, b.Transactions(), numTxs, "%T.Transactions()", b)

	require.NoError(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	require.Lenf(t, b.Receipts(), numTxs, "%T.Receipts()", b)
	for i, r := range b.Receipts() {
		require.Equalf(t, types.ReceiptStatusSuccessful, r.Status, "%T.Receipts()[%d].Status", i, b)
	}

	t.Run("no_tx_replay", func(t *testing.T) {
		// If the tx-inclusion logic were broken then this would include the
		// transactions again, resulting in a FATAL in the execution loop due to
		// non-increasing nonce.
		b := sut.runConsensusLoopOnPreference(t, b)
		assert.Emptyf(t, b.Transactions(), "%T.Transactions()", b)
		require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	})

	t.Run("post_execution_state", func(t *testing.T) {
		sdb := sut.stateAt(t, b.PostExecutionStateRoot())

		got := sdb.GetBalance(recipient)
		want := new(uint256.Int).Mul(transfer, uint256.NewInt(numTxs))
		require.Equalf(t, want, got, "%T.GetBalance(...)", sdb)
	})
}

// TestCanCreateContractSoftError verifies that a CanCreateContract rejection
// results in a failed receipt, not a fatal execution error.
func TestCanCreateContractSoftError(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	stub := &libevmhookstest.Stub{
		CanCreateContractFn: func(*libevm.AddressContext, uint64, libevm.StateReader) (uint64, error) {
			return 0, errors.New("contract creation blocked")
		},
	}
	stub.Register(t)

	const gasLimit uint64 = 100_000
	tx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        nil, // contract creation
		Gas:       gasLimit,
		GasFeeCap: big.NewInt(1),
	})

	b := sut.runConsensusLoop(t, tx)
	require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	require.Lenf(t, b.Receipts(), 1, "%T.Receipts()", b)

	r := b.Receipts()[0]
	assert.Equalf(t, types.ReceiptStatusFailed, r.Status, "%T.Status (contract creation should fail)", r)
	assert.Equalf(t, gasLimit, r.GasUsed, "%T.GasUsed == limit because CanCreateContract returns 0 gas remaining", r)

	// Verify the sender's nonce was incremented despite the failure.
	sdb := sut.stateAt(t, b.PostExecutionStateRoot())
	assert.Equalf(t, uint64(1), sdb.GetNonce(sut.wallet.Addresses()[0]), "%T.GetNonce([sender]) after blocked contract creation", sdb)
}

func TestEmptyChainConfig(t *testing.T) {
	_, sut := newSUT(t, 1, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Config = &params.ChainConfig{
			ChainID: big.NewInt(42),
		}
	}))
	for range 5 {
		sut.runConsensusLoop(t)
	}
}

func TestSyntacticBlockChecks(t *testing.T) {
	ctx, sut := newSUT(t, 0)

	const now = 1e6
	sut.rawVM.config.Now = func() time.Time {
		return time.Unix(now, 0)
	}

	tests := []struct {
		name    string
		header  *types.Header
		wantErr error
	}{
		{
			name: "block_height_overflow_protection",
			header: &types.Header{
				Number: new(big.Int).Lsh(big.NewInt(1), 64),
			},
			wantErr: errBlockHeightNotUint64,
		},
		{
			name: "block_time_at_maximum",
			header: &types.Header{
				Number: big.NewInt(1),
				Time:   now + maxFutureBlockSeconds,
			},
		},
		{
			name: "block_time_after_maximum",
			header: &types.Header{
				Number: big.NewInt(1),
				Time:   now + maxFutureBlockSeconds + 1,
			},
			wantErr: errBlockTooFarInFuture,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := blockstest.NewBlock(t, types.NewBlockWithHeader(tt.header), nil, nil)
			_, err := sut.ParseBlock(ctx, b.Bytes())
			assert.ErrorIs(t, err, tt.wantErr, "ParseBlock(#%v @ time %v) when stubbed time is %d", tt.header.Number, tt.header.Time, uint64(now))
		})
	}
}

func TestAcceptBlock(t *testing.T) {
	// We use a generous timeout because GC finalizers from previous tests take
	// a long time to run in resource constrained environments.
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		runtime.GC()
		require.Zero(t, blocks.InMemoryBlockCount(), "initial in-memory block count")
	}, 5*time.Second, 50*time.Millisecond)

	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

	ctx, sut := newSUT(t, 1, opt)
	// Causes [VM.AcceptBlock] to wait until the block has executed.
	require.NoError(t, sut.SetState(ctx, snow.Bootstrapping), "SetState(Bootstrapping)")

	unsettled := []*blocks.Block{sut.genesis}
	sut.genesis = nil // allow it to be GCd when appropriate

	rng := rand.New(rand.NewPCG(0, 0)) //nolint:gosec // Reproducibility is useful for tests
	for range 100 {
		ffMillis := 100 + rng.IntN(1000*(1+saeparams.TauSeconds))
		vmTime.advance(time.Millisecond * time.Duration(ffMillis))

		b := sut.runConsensusLoop(t)
		unsettled = append(unsettled, b)
		sut.assertBlockHashInvariants(ctx, t)

		lastSettled := b.LastSettled().Height()
		var wantInMemory set.Set[uint64]
		for i, bb := range unsettled {
			switch {
			case bb == nil: // settled earlier
			case bb.Settled():
				unsettled[i] = nil
				require.LessOrEqual(t, bb.Height(), lastSettled, "height of settled block")

			default:
				require.Greater(t, bb.Height(), lastSettled, "height of unsettled block")
				wantInMemory.Add(
					bb.Height(),
					bb.ParentBlock().Height(),
					bb.LastSettled().Height(),
				)
			}
		}

		assert.EventuallyWithT(t, func(t *assert.CollectT) {
			runtime.GC()
			require.Equal(t, int64(wantInMemory.Len()), blocks.InMemoryBlockCount(), "in-memory block count")
		}, 100*time.Millisecond, time.Millisecond)
	}
}

func TestSemanticBlockChecks(t *testing.T) {
	const now = 1e6
	opt, _ := withVMTime(t, time.Unix(now, 0))
	ctx, sut := newSUT(t, 1, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Timestamp = now
	}), opt)

	lastAccepted := sut.lastAcceptedBlock(t)
	tests := []struct {
		name           string
		parentHash     common.Hash // defaults to lastAccepted Hash if zero
		acceptedHeight bool        // if true, block height == lastAccepted.Height(); else +1
		time           uint64      // defaults to `now` if zero
		receipts       types.Receipts
		wantErr        error
	}{
		{
			name:       "unknown_parent",
			parentHash: common.Hash{1},
			wantErr:    errUnknownParent,
		},
		{
			name:           "already_finalized",
			acceptedHeight: true,
			wantErr:        errBlockHeightTooLow,
		},
		{
			name:    "block_time_under_minimum",
			time:    saeparams.TauSeconds - 1,
			wantErr: errBlockTimeUnderMinimum,
		},
		{
			name:    "block_time_before_parent",
			time:    lastAccepted.BuildTime() - 1,
			wantErr: errBlockTimeBeforeParent,
		},
		{
			name:    "block_time_after_maximum",
			time:    now + maxFutureBlockSeconds + 1,
			wantErr: errBlockTimeAfterMaximum,
		},
		{
			name: "hash_mismatch",
			receipts: types.Receipts{
				&types.Receipt{}, // Unexpected receipt
			},
			wantErr: errHashMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.parentHash == (common.Hash{}) {
				tt.parentHash = common.Hash(lastAccepted.ID())
			}
			height := lastAccepted.Height()
			if !tt.acceptedHeight {
				height++
			}
			if tt.time == 0 {
				tt.time = now
			}

			ethB := types.NewBlock(
				&types.Header{
					ParentHash: tt.parentHash,
					Number:     new(big.Int).SetUint64(height),
					Time:       tt.time,
				},
				nil, // txs
				nil, // uncles
				tt.receipts,
				saetest.TrieHasher(),
			)
			b := blockstest.NewBlock(t, ethB, nil, nil)
			require.ErrorIs(t, sut.rawVM.VerifyBlock(ctx, nil, b), tt.wantErr, "VerifyBlock()")
		})
	}
}

func requireReceiveTx(tb testing.TB, nodes []*SUT, txHash common.Hash) {
	tb.Helper()
	for _, sut := range nodes {
		assert.Eventuallyf(tb, func() bool {
			return sut.rawVM.mempool.Has(ids.ID(txHash))
		}, 5*time.Second, 100*time.Millisecond, "tx %x not gossiped to node %s", txHash, sut.nodeID())
	}
	if tb.Failed() {
		tb.FailNow()
	}
}

func requireNotReceiveTx(tb testing.TB, nodes []*SUT, txHash common.Hash) {
	tb.Helper()
	for _, sut := range nodes {
		assert.False(tb, sut.rawVM.mempool.Has(ids.ID(txHash)), "tx %x was gossiped to node %s", txHash, sut.nodeID())
	}
	if tb.Failed() {
		tb.FailNow()
	}
}

func TestGossip(t *testing.T) {
	n := newNetworkedSUTs(t, 2, 2)

	nonValidators := n.allNonValidators()
	api := nonValidators[0]
	tx := api.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &common.Address{},
		Gas:       params.TxGas,
		GasFeeCap: big.NewInt(1),
		Value:     big.NewInt(1),
	})
	api.mustSendTx(t, tx)
	requireReceiveTx(t, n.allValidators(), tx.Hash())
	requireNotReceiveTx(t, nonValidators[1:], tx.Hash())
}

func TestBlockSources(t *testing.T) {
	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(t, 1, opt)

	genesis := sut.lastAcceptedBlock(t)
	// Once a block is settled, its ancestors are only accessible from the
	// database.
	onDisk := sut.runConsensusLoop(t)
	settled := sut.runConsensusLoop(t)
	vmTime.advanceToSettle(ctx, t, settled)
	unsettled := sut.runConsensusLoop(t)

	verified := sut.createAndVerifyBlock(t, unsettled)
	unverified := sut.buildAndParseBlock(t, unwrap(t, verified))

	tests := []struct {
		name            string
		block           *blocks.Block
		wantGetBlockErr testerr.Want
	}{
		{"genesis", genesis, nil},
		{"on_disk", onDisk, nil},
		{"settled_in_memory", settled, nil},
		{"unsettled", unsettled, nil},
		{"verified", unwrap(t, verified), nil},
		{"unverified", unwrap(t, unverified), testerr.Equals(database.ErrNotFound)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("GetBlock", func(t *testing.T) {
				got, err := sut.GetBlock(ctx, tt.block.ID())
				if diff := testerr.Diff(err, tt.wantGetBlockErr); diff != "" {
					t.Fatalf("GetBlock(...) %s", diff)
				}
				if tt.wantGetBlockErr != nil {
					return
				}
				if diff := cmp.Diff(tt.block, unwrap(t, got), blocks.CmpOpt()); diff != "" {
					t.Errorf("GetBlock(...) diff (-want +got):\n%s", diff)
				}
			})

			wantOK := tt.wantGetBlockErr == nil
			opts := cmp.Options{
				cmputils.Blocks(),
				cmputils.Headers(),
				cmpopts.EquateEmpty(),
			}
			t.Run("EthBlockSource", func(t *testing.T) {
				got, gotOK := sut.rawVM.ethBlockSource(tt.block.Hash(), tt.block.NumberU64())
				require.Equalf(t, wantOK, gotOK, "%T.ethBlockSource(...)", sut.rawVM)
				if !wantOK {
					return
				}
				if diff := cmp.Diff(tt.block.EthBlock(), got, opts); diff != "" {
					t.Errorf("%T.ethBlockSource(...) diff (-want +got)\n%s", sut.rawVM, diff)
				}
			})
			t.Run("HeaderSource", func(t *testing.T) {
				got, gotOK := sut.rawVM.headerSource(tt.block.Hash(), tt.block.NumberU64())
				require.Equalf(t, wantOK, gotOK, "%T.headerSource(...)", sut.rawVM)
				if !wantOK {
					return
				}
				if diff := cmp.Diff(tt.block.Header(), got, opts); diff != "" {
					t.Errorf("%T.headerSource(...) diff (-want +got)\n%s", sut.rawVM, diff)
				}
			})
		})
	}
}
