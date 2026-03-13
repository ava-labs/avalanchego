// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"runtime/debug"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/libevm"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/libevm/hookstest"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/rpc"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/escrow"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	ethereum "github.com/ava-labs/libevm"
)

var zeroAddr common.Address

type rpcTest struct {
	method       string
	args         []any
	want         any // untyped nil means no return value.
	wantErr      testerr.Want
	parallel     bool
	eventually   bool
	extraCmpOpts []cmp.Option
}

func (s *SUT) testRPC(ctx context.Context, t *testing.T, tcs ...rpcTest) {
	t.Helper()
	opts := []cmp.Option{
		cmputils.NilSlicesAreEmpty[hexutil.Bytes](),
		cmputils.IfIn[params.ChainConfig](cmp.Options{
			cmputils.BigInts(),
			cmpopts.IgnoreUnexported(params.ChainConfig{}),
		}),
		cmputils.Headers(),
		cmputils.HexutilBigs(),
		cmputils.TransactionsByHash(),
		cmputils.Receipts(),
	}

	for _, tc := range tcs {
		test := func(t require.TestingT) {
			if tc.want == nil { // Reminder: only applies to untyped nil
				tc.want = struct{ json.RawMessage }{} // struct avoids nil vs empty
			}

			got := reflect.New(reflect.TypeOf(tc.want))
			err := s.CallContext(ctx, got.Interface(), tc.method, tc.args...)
			if diff := testerr.Diff(err, tc.wantErr); diff != "" {
				t.Errorf("CallContext(...) %s", diff)
				t.FailNow()
			}
			opts := append(opts, tc.extraCmpOpts...)
			if diff := cmp.Diff(tc.want, got.Elem().Interface(), opts...); diff != "" {
				t.Errorf("Unmarshalled %T diff (-want +got):\n%s", got.Elem().Interface(), diff)
			}
		}

		t.Run(tc.method, func(t *testing.T) {
			if tc.parallel {
				t.Parallel()
			}
			t.Logf("%T.CallContext(ctx, %T, %q, %v...)", s.rpcClient, &tc.want, tc.method, tc.args)
			if tc.eventually {
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					test(c)
				}, time.Second, 10*time.Millisecond)
			} else {
				test(t)
			}
		})
	}
}

// testRPCGetter allows testing of RPC methods for which the return types are
// difficult to unmarshal but there exists a helper such as [ethclient.Client].
// [SUT.testRPC] SHOULD be preferred.
func testRPCGetter[
	Arg any,
	T interface {
		// Only add extra types if JSON unmarshalling from RPC methods directly
		// into the type will fail.
		*types.Block
	},
](ctx context.Context, t *testing.T, underlyingRPCMethod string, get func(context.Context, Arg) (T, error), arg Arg, want T) {
	t.Helper()
	opts := []cmp.Option{
		cmputils.Blocks(),
		cmputils.Headers(),
		cmpopts.EquateEmpty(),
	}

	t.Run(underlyingRPCMethod, func(t *testing.T) {
		got, err := get(ctx, arg)
		t.Logf("%s(%v)", underlyingRPCMethod, arg)
		require.NoErrorf(t, err, "%s(%v)", underlyingRPCMethod, arg)
		if diff := cmp.Diff(want, got, opts...); diff != "" {
			t.Errorf("%s(%v) diff (-want +got):\n%s", underlyingRPCMethod, arg, diff)
		}
	})
}

func TestSubscriptions(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	var (
		newTxs   = make(chan common.Hash, 1)
		newHeads = make(chan *types.Header, 1)
		newLogs  = make(chan types.Log, 1)
	)
	{
		sub, err := sut.rpcClient.EthSubscribe(ctx, newTxs, "newPendingTransactions")
		require.NoError(t, err, "EthSubscribe(newPendingTransactions)")
		t.Cleanup(sub.Unsubscribe)
	}
	{
		sub, err := sut.SubscribeNewHead(ctx, newHeads)
		require.NoError(t, err, "SubscribeNewHead()")
		t.Cleanup(sub.Unsubscribe)
	}
	{
		sub, err := sut.SubscribeFilterLogs(ctx, ethereum.FilterQuery{}, newLogs)
		require.NoError(t, err, "SubscribeFilterLogs()")
		t.Cleanup(sub.Unsubscribe)
	}
	{
		pendingLogs := make(chan types.Log, 1)
		pendingBlock := big.NewInt(int64(rpc.PendingBlockNumber))
		sub, err := sut.SubscribeFilterLogs(
			ctx,
			ethereum.FilterQuery{
				FromBlock: pendingBlock,
				ToBlock:   pendingBlock,
			},
			pendingLogs,
		)
		require.NoError(t, err, "SubscribeFilterLogs(pending)")
		t.Cleanup(sub.Unsubscribe)
		defer func() {
			t.Helper()

			select {
			case l := <-pendingLogs:
				t.Fatalf("unexpected pending log %+v", l)
			default:
			}
		}()
	}

	mustSendTx := func(tx *types.Transaction) {
		t.Helper()

		sut.mustSendTx(t, tx)
		require.Equal(t, tx.Hash(), <-newTxs, "tx hash from newPendingTransactions subscription")
	}

	runConsensusLoop := func(wantLogs ...types.Log) {
		t.Helper()

		b := sut.runConsensusLoop(t)
		require.Equal(t, b.Hash(), (<-newHeads).Hash(), "header hash from newHeads subscription")

		for _, want := range wantLogs {
			want.BlockNumber = b.NumberU64()
			want.BlockHash = b.Hash()
			require.Equal(t, want, <-newLogs, "log from subscription")
		}
	}

	const senderIndex = 0
	deployTx := sut.wallet.SetNonceAndSign(t, senderIndex, &types.LegacyTx{
		Data:     escrow.CreationCode(),
		GasPrice: big.NewInt(1),
		Gas:      3e6,
	})
	mustSendTx(deployTx)
	runConsensusLoop()

	sender := sut.wallet.Addresses()[senderIndex]
	contractAddr := crypto.CreateAddress(sender, deployTx.Nonce())
	amount := uint256.NewInt(100)
	depositTx := sut.wallet.SetNonceAndSign(t, senderIndex, &types.LegacyTx{
		To:       &contractAddr,
		Value:    amount.ToBig(),
		Data:     escrow.CallDataToDeposit(sender),
		GasPrice: big.NewInt(1),
		Gas:      1e6,
	})
	mustSendTx(depositTx)

	wantLog := escrow.DepositEvent(sender, amount)
	wantLog.Address = contractAddr
	wantLog.TxHash = depositTx.Hash()
	runConsensusLoop(*wantLog)
}

func TestWeb3Namespace(t *testing.T) {
	var (
		preImage hexutil.Bytes = []byte("test")
		digest                 = hexutil.Bytes(crypto.Keccak256(preImage))
	)

	ctx, sut := newSUT(t, 1)
	sut.testRPC(ctx, t, []rpcTest{
		{
			method: "web3_clientVersion",
			want:   version.GetVersions().String(),
		},
		{
			method: "web3_sha3",
			args:   []any{preImage},
			want:   digest,
		},
	}...)
}

func TestNetNamespace(t *testing.T) {
	testRPCMethodsWithPeers := func(sut *SUT, wantPeerCount hexutil.Uint) {
		t.Helper()
		sut.testRPC(sut.context(t), t, []rpcTest{
			{
				method: "net_listening",
				want:   true,
			},
			{
				method: "net_peerCount",
				want:   wantPeerCount,
			},
			{
				method: "net_version",
				want:   saetest.ChainConfig().ChainID.String(),
			},
		}...)
	}

	_, sut := newSUT(t, 1) // No peers
	testRPCMethodsWithPeers(sut, 0)

	const (
		numValidators    = 1
		numNonValidators = 2
	)
	n := newNetworkedSUTs(t, numValidators, numNonValidators)
	for _, sut := range n.validators {
		testRPCMethodsWithPeers(sut, numValidators+numNonValidators-1)
	}
	for _, sut := range n.nonValidators {
		testRPCMethodsWithPeers(sut, numValidators)
	}
}

func TestTxPoolNamespace(t *testing.T) {
	ctx, sut := newSUT(t, 2)

	addresses := sut.wallet.Addresses()
	makeTx := func(i int) *types.Transaction {
		t.Helper()
		return sut.wallet.SetNonceAndSign(t, i, &types.DynamicFeeTx{
			To:        &addresses[i],
			Gas:       params.TxGas + uint64(i), //#nosec G115 -- Won't overflow
			GasFeeCap: big.NewInt(int64(i + 1)),
			Value:     big.NewInt(int64(i + 10)),
		})
	}

	const (
		pendingAccount = 0
		queuedAccount  = 1
	)
	pendingTx := makeTx(pendingAccount)
	pendingRPCTx := ethapi.NewRPCPendingTransaction(pendingTx, nil, saetest.ChainConfig())

	_ = makeTx(queuedAccount) // skip the nonce to gap the mempool
	queuedTx := makeTx(queuedAccount)
	queuedRPCTx := ethapi.NewRPCPendingTransaction(queuedTx, nil, saetest.ChainConfig())

	sut.mustSendTx(t, queuedTx, pendingTx)
	sut.waitUntilTxsPending(t, pendingTx)

	// TODO: This formatting is copied from libevm, consider exposing it somehow
	// or removing the dependency on the exact format.
	txToSummary := func(tx *types.Transaction) string {
		return fmt.Sprintf("%s: %d wei + %d gas × %d wei",
			tx.To(),
			tx.Value().Uint64(),
			tx.Gas(),
			tx.GasPrice(),
		)
	}

	sut.testRPC(ctx, t, []rpcTest{
		{
			method: "txpool_content",
			want: map[string]map[string]map[string]*ethapi.RPCTransaction{
				"pending": {
					addresses[pendingAccount].Hex(): {
						"0": pendingRPCTx,
					},
				},
				"queued": {
					addresses[queuedAccount].Hex(): {
						"1": queuedRPCTx,
					},
				},
			},
		},
		{
			method: "txpool_contentFrom",
			args:   []any{addresses[pendingAccount]},
			want: map[string]map[string]*ethapi.RPCTransaction{
				"pending": {
					"0": pendingRPCTx,
				},
				"queued": {},
			},
		},
		{
			method: "txpool_contentFrom",
			args:   []any{addresses[queuedAccount]},
			want: map[string]map[string]*ethapi.RPCTransaction{
				"pending": {},
				"queued": {
					"1": queuedRPCTx,
				},
			},
		},
		{
			method: "txpool_inspect",
			want: map[string]map[string]map[string]string{
				"pending": {
					addresses[pendingAccount].Hex(): {
						"0": txToSummary(pendingTx),
					},
				},
				"queued": {
					addresses[queuedAccount].Hex(): {
						"1": txToSummary(queuedTx),
					},
				},
			},
		},
		{
			method: "txpool_status",
			want: map[string]hexutil.Uint{
				"pending": 1,
				"queued":  1,
			},
		},
	}...)
}

func TestFilterAPIs(t *testing.T) {
	ctx, sut := newSUT(t, 1)
	precompile := common.Address{'f', 'i', 'l', 't'}
	stub := &hookstest.Stub{
		PrecompileOverrides: map[common.Address]libevm.PrecompiledContract{
			precompile: vm.NewStatefulPrecompile(func(env vm.PrecompileEnvironment, _ []byte) ([]byte, error) {
				env.StateDB().AddLog(&types.Log{
					Address: env.Addresses().EVMSemantic.Self,
					Topics:  []common.Hash{},
				})
				return nil, nil
			}),
		},
	}
	stub.Register(t)

	createFilter := func(t *testing.T, method string, args ...any) string {
		t.Helper()
		var filterID string
		require.NoErrorf(t, sut.CallContext(ctx, &filterID, method, args...), "%T.Client.CallContext(..., %q, %v...)", sut.Client, method, args)
		require.NotEmptyf(t, filterID, "Resulted populated by %T.Client.CallContext(..., %q, %v...)", sut.Client, method, args)
		return filterID
	}

	var (
		txFilterID    = createFilter(t, "eth_newPendingTransactionFilter")
		blockFilterID = createFilter(t, "eth_newBlockFilter")
		logFilterID   = createFilter(t, "eth_newFilter", map[string]any{
			"address": precompile,
		})
	)

	defer func() {
		for _, id := range []string{txFilterID, blockFilterID, logFilterID} {
			sut.testRPC(ctx, t, rpcTest{
				method: "eth_uninstallFilter",
				args:   []any{id},
				want:   true,
			})
		}
	}()

	sut.testRPC(ctx, t, []rpcTest{
		{
			method: "eth_getFilterChanges",
			args:   []any{txFilterID},
			want:   []common.Hash{},
		},
		{
			method: "eth_getFilterChanges",
			args:   []any{blockFilterID},
			want:   []common.Hash{},
		},
		{
			method: "eth_getFilterChanges",
			args:   []any{logFilterID},
			want:   []types.Log{},
		},
		{
			method: "eth_getFilterLogs",
			args:   []any{logFilterID},
			want:   []types.Log{},
		},
	}...)

	tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &precompile,
		GasPrice: big.NewInt(1),
		Gas:      1e6,
	})
	sut.sendTxsAndWaitUntilPending(t, tx)
	sut.testRPC(ctx, t, rpcTest{
		method:     "eth_getFilterChanges",
		args:       []any{txFilterID},
		want:       []common.Hash{tx.Hash()},
		eventually: true,
	})

	b := sut.runConsensusLoop(t)
	wantLog := types.Log{
		Address:     precompile,
		Topics:      []common.Hash{},
		BlockNumber: b.NumberU64(),
		BlockHash:   b.Hash(),
		TxHash:      tx.Hash(),
	}
	// getFilterChanges gets accumulated changes, so a second call with
	// the same ID returns empty, as there are no new changes.
	sut.testRPC(ctx, t, []rpcTest{
		// blockFilterID: new block hash available since last poll
		{
			method:     "eth_getFilterChanges",
			args:       []any{blockFilterID},
			want:       []common.Hash{b.Hash()},
			eventually: true,
		},
		{
			method: "eth_getFilterChanges",
			args:   []any{blockFilterID},
			want:   []common.Hash{},
		},

		// logFilterID: new log from block execution available since last poll
		{
			method:     "eth_getFilterChanges",
			args:       []any{logFilterID},
			want:       []types.Log{wantLog},
			eventually: true,
		},
		{
			method: "eth_getFilterChanges",
			args:   []any{logFilterID},
			want:   []types.Log{},
		},
		// getFilterLogs returns all matching logs regardless of prior polling
		// because it is based on block-range criteria, not "changes".
		{
			method: "eth_getFilterLogs",
			args:   []any{logFilterID},
			want:   []types.Log{wantLog},
		},
	}...)
}

func TestEthSyncing(t *testing.T) {
	ctx, sut := newSUT(t, 1)
	// Avalanchego does not expose APIs until after the node has fully synced,
	// so eth_syncing always returns false (not syncing).
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_syncing",
		want:   false,
	})
}

func TestChainID(t *testing.T) {
	for id := range uint64(2) {
		ctx, sut := newSUT(t, 0, options.Func[sutConfig](func(c *sutConfig) {
			c.genesis.Config = &params.ChainConfig{
				ChainID: new(big.Int).SetUint64(id),
			}
		}))
		sut.testRPC(ctx, t, rpcTest{
			method: "eth_chainId",
			want:   hexutil.Uint64(id),
		})
	}
}

func TestEthGetters(t *testing.T) {
	timeOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	blockingPrecompile := common.Address{'b', 'l', 'o', 'c', 'k'}
	precompileOpt, unblock := withBlockingPrecompile(blockingPrecompile)
	ctx, sut := newSUT(t, 1, timeOpt, precompileOpt, withDebugAPI())
	t.Cleanup(unblock)

	t.Run("unknown_hashes", func(t *testing.T) {
		sut.testGetByUnknownHash(ctx, t)
	})
	t.Run("unknown_numbers", func(t *testing.T) {
		sut.testGetByUnknownNumber(ctx, t)
	})

	genesis := sut.lastAcceptedBlock(t)

	createTx := func(t *testing.T, to common.Address) *types.Transaction {
		t.Helper()
		return sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &to,
			Gas:       params.TxGas,
			GasFeeCap: big.NewInt(1),
		})
	}

	// TODO(arr4n) abstract the construction of blocks at all stages as it's
	// copy-pasta'd elsewhere.

	// Once a block is settled, its ancestors are only accessible from the
	// database.
	onDisk := sut.runConsensusLoop(t, createTx(t, zeroAddr))

	settled := sut.runConsensusLoop(t, createTx(t, zeroAddr))
	vmTime.advanceToSettle(ctx, t, settled)

	executed := sut.runConsensusLoop(t, createTx(t, zeroAddr))
	require.NoErrorf(t, executed.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", executed)

	pending := sut.runConsensusLoop(t, createTx(t, blockingPrecompile))

	for _, b := range []*blocks.Block{genesis, onDisk, settled, executed, pending} {
		t.Run(fmt.Sprintf("block_num_%d", b.Height()), func(t *testing.T) {
			ethB := b.EthBlock()
			sut.testGetByHash(ctx, t, ethB)
			sut.testGetByNumber(ctx, t, ethB, rpc.BlockNumber(b.Number().Int64()))
		})
	}

	t.Run("mempool_tx", func(t *testing.T) {
		mempoolTx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &zeroAddr,
			Gas:       params.TxGas,
			GasFeeCap: big.NewInt(1),
		})
		sut.mustSendTx(t, mempoolTx)
		sut.syncMempool(t)

		marshaled, err := mempoolTx.MarshalBinary()
		require.NoErrorf(t, err, "%T.MarshalBinary()", mempoolTx)

		sut.testRPC(ctx, t, []rpcTest{
			{
				method: "eth_getTransactionByHash",
				args:   []any{mempoolTx.Hash()},
				want:   mempoolTx,
			},
			{
				method: "eth_getRawTransactionByHash",
				args:   []any{mempoolTx.Hash()},
				want:   hexutil.Bytes(marshaled),
			},
			{
				method: "debug_getRawTransaction",
				args:   []any{mempoolTx.Hash()},
				want:   hexutil.Bytes(marshaled),
			},
		}...)
	})

	t.Run("named_blocks", func(t *testing.T) {
		// [ethclient.Client.BlockByNumber] isn't compatible with pending blocks as
		// the geth RPC server strips fields that the client then expects to find.
		sut.testRPC(ctx, t, rpcTest{
			method: "eth_getHeaderByNumber",
			args:   []any{rpc.PendingBlockNumber},
			want:   pending.Header(),
		})

		tests := []struct {
			num  rpc.BlockNumber
			want *blocks.Block
		}{
			{rpc.LatestBlockNumber, executed},
			{rpc.SafeBlockNumber, settled},
			{rpc.FinalizedBlockNumber, settled},
		}

		for _, tt := range tests {
			t.Run(tt.num.String(), func(t *testing.T) {
				sut.testGetByNumber(ctx, t, tt.want.EthBlock(), tt.num)
			})
		}

		sut.testRPC(ctx, t, rpcTest{
			method: "eth_blockNumber",
			want:   hexutil.Uint64(executed.Height()),
		})
	})
}

func TestGetLogs(t *testing.T) {
	// We shorten section size to reduce number of required blocks in the test.
	const bloomSectionSize = 8

	timeOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	rng := crypto.NewKeccakState()
	emitter := common.Address{'l', 'o', 'g'}
	precompile := vm.NewStatefulPrecompile(func(env vm.PrecompileEnvironment, _ []byte) ([]byte, error) {
		data := make([]byte, 8)
		_, _ = rng.Read(data)
		env.StateDB().AddLog(&types.Log{
			Address: env.Addresses().EVMSemantic.Self,
			Data:    data, // Guarantee uniqueness as this is the data under test
		})
		return nil, nil
	})

	ctx, sut := newSUT(t, 1, timeOpt, withBloomSectionSize(bloomSectionSize), withPrecompile(emitter, precompile))
	genesis := sut.lastAcceptedBlock(t)

	txWithLog := func(t *testing.T) *types.Transaction {
		t.Helper()
		return sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       &emitter,
			GasPrice: big.NewInt(1),
			Gas:      1e6,
		})
	}
	txWithoutLog := func(t *testing.T) *types.Transaction {
		t.Helper()
		return sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       &common.Address{},
			GasPrice: big.NewInt(1),
			Gas:      params.TxGas,
		})
	}

	// Create enough blocks to ensure some are indexed.
	// Since a block after this will be settled, these are all settled as well,
	// and therefore moved to disk.
	indexed := make([]*blocks.Block, bloomSectionSize)
	for i := range indexed {
		indexed[i] = sut.runConsensusLoop(t, txWithLog(t))
	}

	settled := sut.runConsensusLoop(t, txWithLog(t))
	vmTime.advanceToSettle(ctx, t, settled)

	noLogs := sut.runConsensusLoop(t, txWithoutLog(t))

	executed := sut.runConsensusLoop(t, txWithLog(t))
	require.NoErrorf(t, executed.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", executed)

	// Although the FiltersAPI will work without any blocks indexed, such a
	// scenario would not test the functionality of the bloom indexer.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		be := sut.rawVM.GethRPCBackends()
		_, got := be.BloomStatus()
		require.Equal(c, uint64(1), got, "%T.BloomStatus() sections", be)
	}, 5*time.Second, 100*time.Millisecond, "bloom indexer never finished")

	logsFrom := func(bs ...*blocks.Block) []types.Log {
		var logs []types.Log
		for _, b := range bs {
			for _, r := range b.Receipts() {
				for _, l := range r.Logs {
					logs = append(logs, *l)
				}
			}
		}
		return logs
	}

	ptr := func(h common.Hash) *common.Hash { return &h }

	tests := []struct {
		name     string
		query    ethereum.FilterQuery
		wantLogs []types.Log
		wantErr  testerr.Want
	}{
		{
			name: "genesis",
			query: ethereum.FilterQuery{
				BlockHash: ptr(genesis.Hash()),
			},
		},
		{
			name: "no_logs",
			query: ethereum.FilterQuery{
				BlockHash: ptr(noLogs.Hash()),
			},
		},
		{
			name: "nonexistent_block",
			query: ethereum.FilterQuery{
				BlockHash: &common.Hash{0xde, 0xad},
			},
			wantErr: testerr.Contains("unknown block"),
		},
		{
			name: "on_disk",
			query: ethereum.FilterQuery{
				BlockHash: ptr(indexed[0].Hash()),
			},
			wantLogs: logsFrom(indexed[0]),
		},
		{
			name: "in_memory",
			query: ethereum.FilterQuery{
				BlockHash: ptr(executed.Hash()),
			},
			wantLogs: logsFrom(executed),
		},
		{
			name: "unindexed",
			query: ethereum.FilterQuery{
				FromBlock: settled.Number(),
				ToBlock:   executed.Number(),
				Addresses: []common.Address{emitter},
			},
			wantLogs: logsFrom(settled, executed),
		},
		{
			name: "unknown_contract",
			query: ethereum.FilterQuery{
				FromBlock: settled.Number(),
				ToBlock:   executed.Number(),
				Addresses: []common.Address{{0xf0, 0x00}},
			},
		},
		{
			name: "indexed",
			query: ethereum.FilterQuery{
				FromBlock: indexed[0].Number(),
				ToBlock:   indexed[len(indexed)-1].Number(),
				Addresses: []common.Address{emitter},
			},
			wantLogs: logsFrom(indexed...),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%T: %[1]v", tt.query)
			got, err := sut.FilterLogs(ctx, tt.query)
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("eth_getLogs(...) %s", diff)
			}
			if diff := cmp.Diff(tt.wantLogs, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("eth_getLogs(...) mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestEthPendingTransactions(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	tx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &zeroAddr,
		Gas:       params.TxGas,
		GasFeeCap: big.NewInt(1),
		Value:     big.NewInt(100),
	})
	sut.sendTxsAndWaitUntilPending(t, tx)

	// eth_pendingTransactions filters results to only transactions from
	// accounts configured in the AccountManager, which is always empty.
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_pendingTransactions",
		want:   []*ethapi.RPCTransaction{},
	})
}

func TestGetReceipts(t *testing.T) {
	// Blocking precompile creates accepted-but-not-executed blocks
	blockingPrecompile := common.Address{'b', 'l', 'o', 'c', 'k'}

	timeOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	precompileOpt, unblock := withBlockingPrecompile(blockingPrecompile)
	ctx, sut := newSUT(t, 1, timeOpt, precompileOpt, withDebugAPI())
	t.Cleanup(unblock)

	var (
		txs  []*types.Transaction
		want []*types.Receipt
	)
	for range 6 {
		tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       &zeroAddr,
			Gas:      params.TxGas,
			GasPrice: big.NewInt(1),
		})
		txs = append(txs, tx)
		want = append(want, &types.Receipt{
			TxHash:            tx.Hash(),
			Status:            types.ReceiptStatusSuccessful,
			GasUsed:           params.TxGas,
			EffectiveGasPrice: big.NewInt(1),
			Logs:              []*types.Log{},
		})
	}

	slice := func(t *testing.T, from, to int) (*blocks.Block, []*types.Receipt) {
		t.Helper()
		b := sut.runConsensusLoop(t, txs[from:to]...)
		rs := want[from:to]

		var totalGas uint64
		for i, r := range rs {
			totalGas += r.GasUsed
			r.CumulativeGasUsed = totalGas
			r.BlockHash = b.Hash()
			r.BlockNumber = b.Number()
			r.TransactionIndex = uint(i) //#nosec G115 -- Known non-negative
		}
		return b, rs
	}

	genesis := sut.lastAcceptedBlock(t)

	onDisk, wantOnDisk := slice(t, 0, 2)
	settled, wantSettled := slice(t, 2, 4)
	vmTime.advanceToSettle(ctx, t, settled)
	unsettled, wantUnsettled := slice(t, 4, 6)
	require.NoError(t, unsettled.WaitUntilExecuted(ctx), "WaitUntilExecuted()")

	pending := sut.runConsensusLoop(t, sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &blockingPrecompile,
		Gas:      params.TxGas,
		GasPrice: big.NewInt(1),
	}))

	rawReceiptsFrom := func(b *blocks.Block) []hexutil.Bytes {
		receipts := b.Receipts()
		raw := make([]hexutil.Bytes, len(receipts))
		for i, r := range receipts {
			buf, err := r.MarshalBinary()
			require.NoErrorf(t, err, "receipts[%d].MarshalBinary()", i)
			raw[i] = buf
		}
		return raw
	}

	var tests []rpcTest
	for _, tc := range []struct {
		id      rpc.BlockNumberOrHash
		want    []*types.Receipt
		wantRaw []hexutil.Bytes
	}{
		{
			id:      rpc.BlockNumberOrHashWithHash(onDisk.Hash(), true),
			want:    wantOnDisk,
			wantRaw: rawReceiptsFrom(onDisk),
		},
		{
			id:      rpc.BlockNumberOrHashWithHash(settled.Hash(), true),
			want:    wantSettled,
			wantRaw: rawReceiptsFrom(settled),
		},
		{
			id:      rpc.BlockNumberOrHashWithHash(unsettled.Hash(), true),
			want:    wantUnsettled,
			wantRaw: rawReceiptsFrom(unsettled),
		},
		{
			id:      rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber),
			want:    wantUnsettled,
			wantRaw: rawReceiptsFrom(unsettled),
		},
	} {
		tests = append(tests, rpcTest{
			method: "eth_getBlockReceipts",
			args:   []any{tc.id.String()},
			want:   tc.want,
		}, rpcTest{
			method: "debug_getRawReceipts",
			args:   []any{tc.id.String()},
			want:   tc.wantRaw,
		})
	}

	for i, tx := range txs {
		tests = append(tests, rpcTest{
			method: "eth_getTransactionReceipt",
			args:   []any{tx.Hash()},
			want:   want[i],
		})
	}

	// Acceptance writes blocks to the DB but not receipts, so pending
	// block receipts error, while pending tx receipts block until they're ready
	// as long as they have been included in a block.
	tests = append(tests, []rpcTest{
		{
			method: "eth_getTransactionReceipt",
			args:   []any{common.Hash{}},
			want:   (*types.Receipt)(nil),
		},
		{
			method: "eth_getBlockReceipts",
			args:   []any{common.Hash{}},
			want:   ([]*types.Receipt)(nil),
		},
		{
			method: "debug_getRawReceipts",
			args:   []any{common.Hash{}},
			want:   []hexutil.Bytes{},
		},
		{
			method: "eth_getBlockReceipts",
			args:   []any{genesis.Hash()},
			want:   []*types.Receipt{},
		},
		{
			method: "debug_getRawReceipts",
			args:   []any{genesis.Hash()},
			want:   []hexutil.Bytes{},
		},
		{
			method: "eth_getBlockReceipts",
			args:   []any{pending.Hash()},
			want:   ([]*types.Receipt)(nil),
		},
		{
			method: "debug_getRawReceipts",
			args:   []any{pending.Hash()},
			want:   []hexutil.Bytes{},
		},
		{
			method: "eth_getBlockReceipts",
			args:   []any{hexutil.Uint64(pending.Height())},
			want:   ([]*types.Receipt)(nil),
		},
		{
			method: "debug_getRawReceipts",
			args:   []any{hexutil.Uint64(pending.Height())},
			want:   []hexutil.Bytes{},
		},
	}...)

	sut.testRPC(ctx, t, tests...)
}

func TestGetTransactionCount(t *testing.T) {
	ctx, sut := newSUT(t, 1)
	addr := sut.wallet.Addresses()[0]

	sut.testRPC(ctx, t, rpcTest{
		method: "eth_getTransactionCount",
		args:   []any{addr, "pending"},
		want:   hexutil.Uint64(0),
	})

	tx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &zeroAddr,
		Gas:       params.TxGas,
		GasFeeCap: big.NewInt(1),
	})
	sut.sendTxsAndWaitUntilPending(t, tx)

	sut.testRPC(ctx, t, rpcTest{
		method: "eth_getTransactionCount",
		args:   []any{addr, "pending"},
		want:   hexutil.Uint64(1),
	})
}

// eth_fillTransaction fills defaults (nonce, gas price) without signing,
// so it succeeds without a keystore.
func TestFillTransaction(t *testing.T) {
	const chainID = 42
	ctx, sut := newSUT(t, 1, options.Func[sutConfig](func(c *sutConfig) {
		cfg := *c.genesis.Config
		cfg.ChainID = big.NewInt(chainID)
		c.genesis.Config = &cfg
	}))

	b := sut.runConsensusLoop(t)
	require.NoError(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)

	to := common.Address{'b', 'o', 'b'}
	const (
		gas   = 56_789
		value = 12_345
	)

	want := func(t *testing.T, nonce uint64) ethapi.SignTransactionResult {
		t.Helper()

		// libevm's internal setDefaults fills: nonce from pool, ChainID from config, and
		// London fee fields from SuggestGasTipCap (MinSuggestedTip=1 wei) and
		// the last block's base fee. geth sets maxFeePerGas to 2*baseFee + tip
		// as "slack" to avoid invalidation if the base fee is rising.
		tip := big.NewInt(1)
		feeCap := new(big.Int).Add(
			tip,
			new(big.Int).Mul(b.Header().BaseFee, big.NewInt(2)),
		)

		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   big.NewInt(chainID),
			Nonce:     nonce,
			To:        &to,
			GasTipCap: tip,
			GasFeeCap: feeCap,
			Gas:       gas,
			Value:     big.NewInt(value),
		})
		raw, err := tx.MarshalBinary()
		require.NoError(t, err, "tx.MarshalBinary()")
		return ethapi.SignTransactionResult{Raw: raw, Tx: tx}
	}

	args := map[string]any{
		"from":  sut.wallet.Addresses()[0],
		"to":    to,
		"gas":   hexutil.Uint64(gas),
		"value": hexBig(value),
	}

	sut.testRPC(ctx, t, rpcTest{
		method: "eth_fillTransaction",
		args:   []any{args},
		want:   want(t, 0),
	})

	// Placing a transaction in the mempool to confirm that the filled nonce is
	// incremented accordingly.
	tx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &zeroAddr,
		Gas:       params.TxGas,
		GasFeeCap: big.NewInt(1),
	})
	sut.sendTxsAndWaitUntilPending(t, tx)

	sut.testRPC(ctx, t, rpcTest{
		method: "eth_fillTransaction",
		args:   []any{args},
		want:   want(t, 1),
	})
}

// eth_resend replaces a pending transaction with updated gas parameters.
// Resend re-signs the replacement transaction server-side, which requires a
// keystore, which SAE does not support, so signing is as far as we can get
// and we verify that the RPC fails in an expected way.
func TestResend(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	// Submit a pending tx so Resend can find it in the pool.
	tx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &zeroAddr,
		Gas:       params.TxGas,
		GasFeeCap: big.NewInt(1),
	})
	sut.sendTxsAndWaitUntilPending(t, tx)

	sut.testRPC(ctx, t, rpcTest{
		method: "eth_resend",
		args: []any{
			map[string]any{
				"from":                 sut.wallet.Addresses()[0],
				"nonce":                hexutil.Uint64(tx.Nonce()),
				"to":                   tx.To(),
				"gas":                  hexutil.Uint64(tx.Gas()),
				"maxFeePerGas":         (*hexutil.Big)(tx.GasFeeCap()),
				"maxPriorityFeePerGas": (*hexutil.Big)(tx.GasTipCap()),
			},
			hexBig(2), // arbitrary
		},
		wantErr: testerr.Contains("unknown account"),
	})
}

// SAE doesn't really support APIs that require a key on the node, as there is
// no way to add keys. But, we want to ensure the methods error gracefully.
func TestEthSigningAPIs(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	wantErr := testerr.Contains("unknown account")
	txFields := map[string]any{
		"from":     zeroAddr,
		"to":       zeroAddr,
		"gas":      hexutil.Uint64(params.TxGas),
		"gasPrice": hexBig(1),
		"value":    hexBig(100),
		"nonce":    hexutil.Uint64(0),
	}
	sut.testRPC(ctx, t, []rpcTest{
		{
			method: "eth_sign",
			args: []any{
				zeroAddr,
				hexutil.Bytes("test message"),
			},
			wantErr: wantErr,
		},
		{
			method: "eth_signTransaction",
			args: []any{
				txFields,
			},
			wantErr: wantErr,
		},
		{
			method: "eth_sendTransaction",
			args: []any{
				txFields,
			},
			wantErr: wantErr,
		},
	}...)
}

func TestRPCTxFeeCap(t *testing.T) {
	tests := []struct {
		name     string
		cap      float64
		gasPrice *big.Int
		wantErr  testerr.Want
	}{
		{
			name:     "under_cap",
			cap:      0.001,
			gasPrice: big.NewInt(params.Wei), // fee = 21000 wei < 0.001 ETH
		},
		{
			name:     "over_cap",
			cap:      0.001,
			gasPrice: big.NewInt(params.Ether), // fee = 21000 ETH > 0.001 ETH
			wantErr:  testerr.Contains("exceeds the configured cap"),
		},
		{
			name:     "no_cap",
			cap:      0, // 0 = no cap
			gasPrice: big.NewInt(params.Ether),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, sut := newSUT(t, 1, withTxFeeCap(tt.cap))
			tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
				To:       &zeroAddr,
				Gas:      params.TxGas,
				GasPrice: tt.gasPrice,
			})
			err := sut.Client.SendTransaction(sut.context(t), tx)
			if diff := testerr.Diff(err, tt.wantErr); diff != "" {
				t.Fatalf("SendTransaction() %s", diff)
			}
		})
	}
}

func TestDebugRPCs(t *testing.T) {
	ctx, sut := newSUT(t, 0, withDebugAPI())

	sut.testRPC(ctx, t, []rpcTest{
		{
			// SAE does not support rewinding - setHead is a no-op.
			method: "debug_setHead",
			args:   []any{hexutil.Uint64(0)},
			want:   json.RawMessage("null"),
		},
		{
			method: "debug_chaindbCompact",
			want:   json.RawMessage("null"),
		},
		{
			method:  "debug_chaindbProperty",
			args:    []any{"leveldb.stats"},
			wantErr: testerr.Contains("not supported"),
		},
		{
			method: "debug_dbGet",
			args:   []any{hexutil.Encode([]byte("LastBlock"))},
			want:   hexutil.Bytes(rawdb.ReadHeadBlockHash(sut.db).Bytes()),
		},
		{
			method:  "debug_dbAncient",
			args:    []any{"headers", uint64(0)},
			wantErr: testerr.Contains("not supported"),
		},
		{
			method:  "debug_dbAncients",
			wantErr: testerr.Contains("not supported"),
		},
		{
			method:  "debug_printBlock",
			args:    []any{uint64(1)}, // SUT only has genesis, so block 1 doesn't exist.
			wantErr: testerr.Contains("not found"),
		},
	}...)

	// The profiling debug namespace is handled entirely by upstream code
	// that doesn't depend in any way on SAE. We therefore only need an
	// integration test, not to exercise every method because such unit
	// testing is the responsibility of the source.
	//
	// Reference: https://geth.ethereum.org/docs/interacting-with-geth/rpc/ns-debug
	t.Run("debug_setGCPercent", func(t *testing.T) {
		const firstArg = 100
		beforeTest := debug.SetGCPercent(firstArg)
		defer debug.SetGCPercent(beforeTest)

		const m = "debug_setGCPercent"
		sut.testRPC(ctx, t, []rpcTest{
			// Invariant: each call returns the input argument of the last.
			{
				method: m,
				args:   []any{42},
				want:   firstArg,
			},
			{
				method: m,
				args:   []any{0},
				want:   42,
			},
		}...)
	})
}

func (s *SUT) testGetByHash(ctx context.Context, t *testing.T, want *types.Block) {
	t.Helper()

	testRPCGetter(ctx, t, "eth_getBlockByHash", s.BlockByHash, want.Hash(), want)

	wantBlockRLP, err := rlp.EncodeToBytes(want)
	require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", want)
	wantHeaderRLP, err := rlp.EncodeToBytes(want.Header())
	require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", want.Header())

	s.testRPC(ctx, t, []rpcTest{
		{
			method: "eth_getBlockByHash",
			args:   []any{want.Hash(), false},
			want:   want.Header(),
		},
		{
			method: "eth_getBlockTransactionCountByHash",
			args:   []any{want.Hash()},
			want:   hexutil.Uint(len(want.Transactions())),
		},
		{
			method: "eth_getUncleByBlockHashAndIndex",
			args:   []any{want.Hash(), hexutil.Uint(0)},
			want:   (map[string]any)(nil), // SAE never has uncles (no reorgs)
		},
		{
			method: "eth_getUncleCountByBlockHash",
			args:   []any{want.Hash()},
			want:   hexutil.Uint(0), // SAE never has uncles (no reorgs)
		},
		{
			method: "debug_getRawBlock",
			args:   []any{want.Hash()},
			want:   hexutil.Bytes(wantBlockRLP),
		},
		{
			method: "debug_getRawHeader",
			args:   []any{want.Hash()},
			want:   hexutil.Bytes(wantHeaderRLP),
		},
	}...)

	for i, wantTx := range want.Transactions() {
		txIdx := hexutil.Uint(i) //#nosec G115 -- Won't overflow
		marshaled, err := wantTx.MarshalBinary()
		require.NoErrorf(t, err, "%T.MarshalBinary()", wantTx)

		s.testRPC(ctx, t, []rpcTest{
			{
				method: "eth_getTransactionByHash",
				args:   []any{wantTx.Hash()},
				want:   wantTx,
			},
			{
				method: "eth_getTransactionByBlockHashAndIndex",
				args:   []any{want.Hash(), txIdx},
				want:   wantTx,
			},
			{
				method: "eth_getRawTransactionByBlockHashAndIndex",
				args:   []any{want.Hash(), txIdx},
				want:   hexutil.Bytes(marshaled),
			},
			{
				method: "eth_getRawTransactionByHash",
				args:   []any{wantTx.Hash()},
				want:   hexutil.Bytes(marshaled),
			},
			{
				method: "debug_getRawTransaction",
				args:   []any{wantTx.Hash()},
				want:   hexutil.Bytes(marshaled),
			},
		}...)
	}

	outOfBoundsIndex := hexutil.Uint(len(want.Transactions()) + 1) //#nosec G115 -- Known to not overflow
	s.testRPC(ctx, t, []rpcTest{
		{
			method: "eth_getTransactionByBlockHashAndIndex",
			args:   []any{want.Hash(), outOfBoundsIndex},
			want:   (*types.Transaction)(nil),
		},
		{
			method: "eth_getRawTransactionByBlockHashAndIndex",
			args:   []any{want.Hash(), outOfBoundsIndex},
			want:   hexutil.Bytes(nil),
		},
	}...)
}

func (s *SUT) testGetByUnknownHash(ctx context.Context, t *testing.T) {
	t.Helper()

	s.testRPC(ctx, t, []rpcTest{
		{
			method: "eth_getBlockByHash",
			args:   []any{common.Hash{}, true},
			want:   (*types.Block)(nil),
		},
		{
			method: "eth_getHeaderByHash",
			args:   []any{common.Hash{}},
			want:   (*types.Header)(nil),
		},
		{
			method: "eth_getBlockTransactionCountByHash",
			args:   []any{common.Hash{}},
			want:   (*hexutil.Uint)(nil),
		},
		{
			method: "eth_getTransactionByBlockHashAndIndex",
			args:   []any{common.Hash{}, hexutil.Uint(0)},
			want:   (*types.Transaction)(nil),
		},
		{
			method: "eth_getRawTransactionByBlockHashAndIndex",
			args:   []any{common.Hash{}, hexutil.Uint(0)},
			want:   hexutil.Bytes(nil),
		},
		{
			method: "eth_getTransactionByHash",
			args:   []any{common.Hash{}},
			want:   (*types.Transaction)(nil),
		},
		{
			method: "eth_getRawTransactionByHash",
			args:   []any{common.Hash{}},
			want:   hexutil.Bytes(nil),
		},
		{
			method: "debug_getRawTransaction",
			args:   []any{common.Hash{}},
			want:   hexutil.Bytes(nil),
		},
		{
			method:  "debug_getRawBlock",
			args:    []any{common.Hash{}},
			wantErr: testerr.Contains("not found"),
		},
		{
			method:  "debug_getRawHeader",
			args:    []any{common.Hash{}},
			wantErr: testerr.Contains("not found"),
		},
	}...)
}

// testGetByNumber accepts a block-number override to allow testing via named
// blocks, e.g. [rpc.LatestBlockNumber], not only via the specific number
// carried by the [types.Block].
func (s *SUT) testGetByNumber(ctx context.Context, t *testing.T, want *types.Block, n rpc.BlockNumber) {
	t.Helper()
	testRPCGetter(ctx, t, "eth_getBlockByNumber", s.BlockByNumber, big.NewInt(n.Int64()), want)

	wantBlockRLP, err := rlp.EncodeToBytes(want)
	require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", want)
	wantHeaderRLP, err := rlp.EncodeToBytes(want.Header())
	require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", want.Header())

	s.testRPC(ctx, t, []rpcTest{
		{
			method: "eth_getBlockByNumber",
			args:   []any{n, false},
			want:   want.Header(),
		},
		{
			method: "eth_getBlockTransactionCountByNumber",
			args:   []any{n},
			want:   hexutil.Uint(len(want.Transactions())),
		},
		{
			method: "eth_getUncleByBlockNumberAndIndex",
			args:   []any{n, hexutil.Uint(0)},
			want:   (map[string]any)(nil), // SAE never has uncles (no reorgs)
		},
		{
			method: "eth_getUncleCountByBlockNumber",
			args:   []any{n},
			want:   hexutil.Uint(0), // SAE never has uncles (no reorgs)
		},
		{
			method: "debug_getRawBlock",
			args:   []any{n},
			want:   hexutil.Bytes(wantBlockRLP),
		},
		{
			method: "debug_getRawHeader",
			args:   []any{n},
			want:   hexutil.Bytes(wantHeaderRLP),
		},
	}...)

	for i, wantTx := range want.Transactions() {
		txIdx := hexutil.Uint(i) //#nosec G115 -- Won't overflow
		marshaled, err := wantTx.MarshalBinary()
		require.NoErrorf(t, err, "%T.MarshalBinary()", wantTx)

		s.testRPC(ctx, t, []rpcTest{
			{
				method: "eth_getTransactionByBlockNumberAndIndex",
				args:   []any{n, txIdx},
				want:   wantTx,
			},
			{
				method: "eth_getRawTransactionByBlockNumberAndIndex",
				args:   []any{n, txIdx},
				want:   hexutil.Bytes(marshaled),
			},
		}...)
	}

	outOfBoundsIndex := hexutil.Uint(len(want.Transactions()) + 1) //#nosec G115 -- Known to not overflow
	s.testRPC(ctx, t, []rpcTest{
		{
			method: "eth_getTransactionByBlockNumberAndIndex",
			args:   []any{n, outOfBoundsIndex},
			want:   (*types.Transaction)(nil),
		},
		{
			method: "eth_getRawTransactionByBlockNumberAndIndex",
			args:   []any{n, outOfBoundsIndex},
			want:   hexutil.Bytes(nil),
		},
	}...)
}

func (s *SUT) testGetByUnknownNumber(ctx context.Context, t *testing.T) {
	t.Helper()

	const n rpc.BlockNumber = math.MaxInt64
	s.testRPC(ctx, t, []rpcTest{
		{
			method: "eth_getBlockByNumber",
			args:   []any{n, true},
			want:   (*types.Block)(nil),
		},
		{
			method: "eth_getHeaderByNumber",
			args:   []any{n},
			want:   (*types.Header)(nil),
		},
		{
			method: "eth_getBlockTransactionCountByNumber",
			args:   []any{n},
			want:   (*hexutil.Uint)(nil),
		},
		{
			method: "eth_getTransactionByBlockNumberAndIndex",
			args:   []any{n, hexutil.Uint(0)},
			want:   (*types.Transaction)(nil),
		},
		{
			method: "eth_getRawTransactionByBlockNumberAndIndex",
			args:   []any{n, hexutil.Uint(0)},
			want:   hexutil.Bytes(nil),
		},
		{
			method: "debug_getRawBlock",
			args:   []any{n},
			want:   hexutil.Bytes(nil),
		},
		{
			method: "debug_getRawHeader",
			args:   []any{n},
			want:   hexutil.Bytes(nil),
		},
	}...)
}

func withTxFeeCap(feeCap float64) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.RPCConfig.TxFeeCap = feeCap
	})
}

// withDebugAPI returns a sutOption that enables the debug API.
func withDebugAPI() sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.vmConfig.RPCConfig.EnableDBInspecting = true
		c.vmConfig.RPCConfig.EnableProfiling = true
	})
}

func TestResolveBlockNumberOrHash(t *testing.T) {
	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(t, 0, opt)

	settled := sut.runConsensusLoop(t)
	vmTime.advanceToSettle(ctx, t, settled)

	for range 2 {
		b := sut.runConsensusLoop(t)
		vmTime.advanceToSettle(ctx, t, b)
	}
	_, ok := sut.rawVM.consensusCritical.Load(settled.Hash())
	require.False(t, ok, "settled block still in VM memory")

	accepted := sut.runConsensusLoop(t)
	require.NoError(t, sut.SetPreference(ctx, accepted.ID()), "SetPreference()")

	b, err := sut.BuildBlock(ctx)
	require.NoError(t, err, "BuildBlock()")
	// Blocks are added to the VM memory with [VM.VerifyBlock] but only become
	// canonical (and on disk) with [VM.AcceptBlock].
	require.NoErrorf(t, b.Verify(ctx), "%T.Verify()", b)
	nonCanonical := unwrap(t, b)

	tests := []struct {
		name     string
		nOrH     rpc.BlockNumberOrHash
		wantNum  uint64
		wantHash common.Hash
		wantErr  error
	}{
		{
			name:    "neither_num_nor_hash",
			wantErr: blocks.ErrNeitherNumberNorHash,
		},
		{
			name: "both_num_and_hash",
			nOrH: rpc.BlockNumberOrHash{
				BlockNumber: utils.PointerTo(rpc.LatestBlockNumber),
				BlockHash:   &common.Hash{},
			},
			wantErr: blocks.ErrBothNumberAndHash,
		},
		{
			name:     "named_block",
			nOrH:     rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber),
			wantNum:  accepted.NumberU64(),
			wantHash: accepted.Hash(),
		},
		{
			name: "canonical_hash_in_memory",
			nOrH: rpc.BlockNumberOrHash{
				BlockHash: utils.PointerTo(accepted.Hash()),
			},
			wantNum:  accepted.NumberU64(),
			wantHash: accepted.Hash(),
		},
		{
			name: "canonical_hash_on_disk",
			nOrH: rpc.BlockNumberOrHash{
				BlockHash: utils.PointerTo(settled.Hash()),
			},
			wantNum:  settled.NumberU64(),
			wantHash: settled.Hash(),
		},
		{
			name: "non_canonical_when_canonical_not_required",
			nOrH: rpc.BlockNumberOrHash{
				BlockHash: utils.PointerTo(nonCanonical.Hash()),
			},
			wantNum:  nonCanonical.NumberU64(),
			wantHash: nonCanonical.Hash(),
		},
		{
			name: "non_canonical_when_canonical_required",
			nOrH: rpc.BlockNumberOrHash{
				BlockHash:        utils.PointerTo(nonCanonical.Hash()),
				RequireCanonical: true,
			},
			wantErr: blocks.ErrNonCanonicalBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain := sut.rawVM.chain()
			gotNum, gotHash, err := blocks.ResolveRPCNumberOrHash(chain, tt.nOrH)
			t.Logf("blocks.ResolveBlockNumberOrhash(%T, %+v)", chain, tt.nOrH) // avoids having to repeat in failure messages
			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantNum, gotNum)
			assert.Equal(t, tt.wantHash, gotHash)
		})
	}
}

func hexBig(n int64) *hexutil.Big {
	return (*hexutil.Big)(big.NewInt(n))
}

func hexBigU(n uint64) *hexutil.Big {
	return (*hexutil.Big)(new(big.Int).SetUint64(n))
}
