// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient/gethclient"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgradechain"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"

	ethereum "github.com/ava-labs/libevm"
)

// newPreSAESUT boots the cchain VM on the [upgradechain] fixture's database,
// modelling a node whose coreth-built history crosses every network upgrade
// and which has just switched to this VM.
func newPreSAESUT(t *testing.T) (context.Context, *SUT, *upgradechain.Fixture) {
	t.Helper()

	fx, err := upgradechain.Load()
	require.NoError(t, err, "upgradechain.Load()")

	// The fixture was dumped from the database handle coreth's VM received,
	// which corresponds to the "chain" prefix of the SUT's base database.
	db := memdb.New()
	chainDB := prefixdb.New([]byte("chain"), db)
	for _, kv := range fx.Database {
		require.NoError(t, chainDB.Put(kv.Key, kv.Value), "seeding fixture database key %x", kv.Key)
	}

	tip := fx.Blocks[len(fx.Blocks)-1]
	timeOpt, _ := withVMTime(time.Unix(int64(tip.Time)+10, 0)) //#nosec G115 -- Known non-negative

	// Schedule the SAE-era Helicon upgrade just past the synchronous tip so
	// that the historical chain remains valid under it.
	upgrades := fx.Upgrades
	upgrades.HeliconTime = time.Unix(int64(tip.Time)+1, 0) //#nosec G115 -- Known non-negative

	ctx, sut := newSUT(t,
		withDB(db),
		withGenesisJSON(fx.Genesis),
		withUpgrades(upgrades),
		timeOpt,
	)
	return ctx, sut, fx
}

// TestPreSAEChainRPCs exercises the stateful RPCs against every block of a
// real coreth-built history spanning all network upgrades. All blocks are
// below the last-settled height and absent from VM memory, so every query
// takes the on-disk restore path for synchronous (pre-SAE) blocks.
func TestPreSAEChainRPCs(t *testing.T) {
	ctx, sut, fx := newPreSAESUT(t)

	tip := fx.Blocks[len(fx.Blocks)-1]
	lastAccepted, err := sut.LastAccepted(ctx)
	require.NoErrorf(t, err, "%T.LastAccepted()", sut.VM)
	assert.Equal(t, tip.Hash, common.Hash(lastAccepted), "VM boots onto the synchronous tip")

	blockNumber, err := sut.ethclient.BlockNumber(ctx)
	require.NoError(t, err, "BlockNumber()")
	assert.Equal(t, tip.Number, blockNumber, "eth_blockNumber reports the synchronous tip")

	blocks := make(map[uint64]*types.Block, len(fx.Blocks))
	for _, fb := range fx.Blocks {
		eth := new(types.Block)
		require.NoError(t, rlp.DecodeBytes(fb.RLP, eth), "rlp.DecodeBytes(block %d)", fb.Number)
		blocks[fb.Number] = eth
	}

	t.Run("genesis", func(t *testing.T) {
		var got map[string]any
		require.NoError(t,
			sut.ethclient.Client().CallContext(ctx, &got, "eth_getBlockByNumber", hexutil.Uint64(0), false),
			"eth_getBlockByNumber(0)")
		assert.Equal(t, blocks[1].ParentHash().Hex(), got["hash"],
			"genesis hash matches the fixture chain's genesis (block 1's parent)")
	})

	for _, fb := range fx.Blocks {
		t.Run(fmt.Sprintf("block_%02d_%s", fb.Number, fb.Fork), func(t *testing.T) {
			t.Logf("%s", fb.Note)
			testBlockRPCs(ctx, t, sut, fx, fb, blocks[fb.Number])
		})
	}
}

func testBlockRPCs(ctx context.Context, t *testing.T, sut *SUT, fx *upgradechain.Fixture, fb upgradechain.Block, eth *types.Block) {
	testBlockLookupRPCs(ctx, t, sut, fb, eth)
	testStateRPCs(ctx, t, sut, fx, fb)
	testReceiptRPCs(ctx, t, sut, fb, eth)
	testTransactionRPCs(ctx, t, sut, fb, eth)
	testTraceBlockRPCs(ctx, t, sut, fb, eth)
	testTraceTransactionRPCs(ctx, t, sut, fb, eth)
	testIntermediateRootsRPC(ctx, t, sut, fb, eth)
	testLogsRPC(ctx, t, sut, fb)
}

// nativeAssetCallTraceBroken reports whether callTracing the block fails with
// exactly [upgradechain.NativeAssetCallTraceError]
func nativeAssetCallTraceBroken(number uint64) bool {
	return slices.Contains(upgradechain.NativeAssetCallBlocks, number)
}

func testBlockLookupRPCs(ctx context.Context, t *testing.T, sut *SUT, fb upgradechain.Block, eth *types.Block) {
	t.Helper()

	t.Run("eth_getBlockBy", func(t *testing.T) {
		byNumber, err := sut.ethclient.BlockByNumber(ctx, new(big.Int).SetUint64(fb.Number))
		require.NoError(t, err, "BlockByNumber(%d)", fb.Number)
		assert.Equal(t, fb.Hash, byNumber.Hash(), "BlockByNumber(%d) hash", fb.Number)

		byHash, err := sut.ethclient.BlockByHash(ctx, fb.Hash)
		require.NoError(t, err, "BlockByHash(%s)", fb.Hash)
		assert.Equal(t, fb.Hash, byHash.Hash(), "BlockByHash(%s) hash", fb.Hash)
		assert.Len(t, byHash.Transactions(), len(eth.Transactions()), "EVM transactions in block")
	})
}

func testStateRPCs(ctx context.Context, t *testing.T, sut *SUT, fx *upgradechain.Fixture, fb upgradechain.Block) {
	t.Helper()

	blockNum := new(big.Int).SetUint64(fb.Number)
	t.Run("balances_and_nonces", func(t *testing.T) {
		for addr, want := range fb.State {
			gotBalance, err := sut.ethclient.BalanceAt(ctx, addr, blockNum)
			require.NoError(t, err, "BalanceAt(%s, %d)", addr, fb.Number)
			assert.Zerof(t, want.Balance.ToInt().Cmp(gotBalance), "BalanceAt(%s, %d): want %s, got %s",
				addr, fb.Number, want.Balance.ToInt(), gotBalance)

			gotNonce, err := sut.ethclient.NonceAt(ctx, addr, blockNum)
			require.NoError(t, err, "NonceAt(%s, %d)", addr, fb.Number)
			assert.Equal(t, want.Nonce, gotNonce, "NonceAt(%s, %d)", addr, fb.Number)
		}
	})

	t.Run("counter_contract", func(t *testing.T) {
		code, err := sut.ethclient.CodeAt(ctx, fx.Counter, blockNum)
		require.NoError(t, err, "CodeAt(counter, %d)", fb.Number)
		assert.NotEmpty(t, code, "counter contract code")

		slot0, err := sut.ethclient.StorageAt(ctx, fx.Counter, common.Hash{}, blockNum)
		require.NoError(t, err, "StorageAt(counter, 0, %d)", fb.Number)
		assert.Equal(t, fb.Counter, new(big.Int).SetBytes(slot0).Uint64(), "counter storage slot 0")

		got, err := sut.ethclient.CallContract(ctx, ethereum.CallMsg{
			To:   &fx.Counter,
			Data: []byte{1}, // any non-empty call data returns slot 0
		}, blockNum)
		require.NoError(t, err, "CallContract(counter, %d)", fb.Number)
		assert.Equal(t, fb.Counter, new(big.Int).SetBytes(got).Uint64(), "eth_call returned counter value")
	})

	t.Run("eth_getProof", func(t *testing.T) {
		proof, err := gethclient.New(sut.ethclient.Client()).GetProof(ctx, fx.Counter, nil, blockNum)
		require.NoError(t, err, "GetProof(counter, %d)", fb.Number)

		want := fb.State[fx.Counter]
		assert.Zero(t, want.Balance.ToInt().Cmp(proof.Balance), "proof balance")
		assert.Equal(t, want.Nonce, proof.Nonce, "proof nonce")
	})
}

func testReceiptRPCs(ctx context.Context, t *testing.T, sut *SUT, fb upgradechain.Block, eth *types.Block) {
	t.Helper()

	wantStatus := types.ReceiptStatusSuccessful
	if fb.Number == upgradechain.DeprecatedNativeAssetCallBlock {
		wantStatus = types.ReceiptStatusFailed
	}

	t.Run("eth_getBlockReceipts", func(t *testing.T) {
		receipts, err := sut.ethclient.BlockReceipts(ctx, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(fb.Number))) //#nosec G115 -- Known non-negative
		require.NoError(t, err, "BlockReceipts(%d)", fb.Number)
		require.Len(t, receipts, len(eth.Transactions()), "receipts per EVM transaction")
		for i, r := range receipts {
			assert.Equal(t, fb.Hash, r.BlockHash, "receipt %d block hash", i)
			assert.Equal(t, eth.Transactions()[i].Hash(), r.TxHash, "receipt %d tx hash", i)
			assert.Equal(t, wantStatus, r.Status, "receipt %d status", i)
		}
	})
}

func testTransactionRPCs(ctx context.Context, t *testing.T, sut *SUT, fb upgradechain.Block, eth *types.Block) {
	t.Helper()

	t.Run("transactions", func(t *testing.T) {
		for i, tx := range eth.Transactions() {
			receipt, err := sut.ethclient.TransactionReceipt(ctx, tx.Hash())
			require.NoError(t, err, "TransactionReceipt(%s)", tx.Hash())
			assert.Equal(t, fb.Hash, receipt.BlockHash, "receipt block hash of tx %d", i)

			got, pending, err := sut.ethclient.TransactionByHash(ctx, tx.Hash())
			require.NoError(t, err, "TransactionByHash(%s)", tx.Hash())
			assert.False(t, pending, "TransactionByHash(%s) is pending", tx.Hash())
			assert.Equal(t, tx.Hash(), got.Hash(), "transaction hash %d", i)
		}
	})
}

func testTraceBlockRPCs(ctx context.Context, t *testing.T, sut *SUT, fb upgradechain.Block, eth *types.Block) {
	t.Helper()

	rpcClient := sut.ethclient.Client()
	t.Run("debug_traceBlock", func(t *testing.T) {
		tracerConfig := map[string]any{"tracer": "callTracer"}
		var traces []struct {
			TxHash common.Hash     `json:"txHash"`
			Result json.RawMessage `json:"result"`
			Error  string          `json:"error"`
		}
		err := rpcClient.CallContext(ctx, &traces, "debug_traceBlockByNumber", hexutil.Uint64(fb.Number), tracerConfig)
		if nativeAssetCallTraceBroken(fb.Number) {
			require.EqualError(t, err, upgradechain.NativeAssetCallTraceError, "debug_traceBlockByNumber(%d) matches coreth", fb.Number)
			err = rpcClient.CallContext(ctx, &traces, "debug_traceBlockByHash", fb.Hash, tracerConfig)
			require.EqualError(t, err, upgradechain.NativeAssetCallTraceError, "debug_traceBlockByHash(%s) matches coreth", fb.Hash)
			return
		}
		require.NoError(t, err, "debug_traceBlockByNumber(%d)", fb.Number)
		require.Len(t, traces, len(eth.Transactions()), "traces per EVM transaction")
		for i, trace := range traces {
			assert.Empty(t, trace.Error, "trace %d error", i)
			assert.NotEmpty(t, trace.Result, "trace %d result", i)
		}

		require.NoError(t,
			rpcClient.CallContext(ctx, &traces, "debug_traceBlockByHash", fb.Hash, tracerConfig),
			"debug_traceBlockByHash(%s)", fb.Hash)
		assert.Len(t, traces, len(eth.Transactions()), "traces per EVM transaction (by hash)")

		assertTraceBlockPrestates(ctx, t, sut, fb, eth)
	})
}

func assertTraceBlockPrestates(ctx context.Context, t *testing.T, sut *SUT, fb upgradechain.Block, eth *types.Block) {
	t.Helper()

	// Every transaction credits the blackhole coinbase with its full fee
	// (gasUsed × effectiveGasPrice), so the coinbase balance each
	// transaction's prestate observes pins the intra-block state handoff.
	var prestates []struct {
		Result map[common.Address]struct {
			Balance *hexutil.Big `json:"balance"`
		} `json:"result"`
	}
	require.NoError(t,
		sut.ethclient.Client().CallContext(ctx, &prestates, "debug_traceBlockByNumber", hexutil.Uint64(fb.Number), map[string]any{"tracer": "prestateTracer"}),
		"debug_traceBlockByNumber(%d, prestateTracer)", fb.Number)
	require.Len(t, prestates, len(eth.Transactions()), "prestates per EVM transaction")

	receipts, err := sut.ethclient.BlockReceipts(ctx, rpc.BlockNumberOrHashWithHash(fb.Hash, false))
	require.NoError(t, err, "BlockReceipts(%s)", fb.Hash)
	require.Len(t, receipts, len(eth.Transactions()), "receipts per EVM transaction")

	coinbase := eth.Coinbase()
	wantBalance, err := sut.ethclient.BalanceAt(ctx, coinbase, new(big.Int).SetUint64(fb.Number-1))
	require.NoError(t, err, "BalanceAt(%s, %d)", coinbase, fb.Number-1)
	for i, r := range receipts {
		pre, ok := prestates[i].Result[coinbase]
		require.True(t, ok, "coinbase %s in prestate of tx %d", coinbase, i)
		assert.Zerof(t, wantBalance.Cmp(pre.Balance.ToInt()),
			"coinbase balance in prestate of tx %d: want %s, got %s", i, wantBalance, pre.Balance.ToInt())

		fee := new(big.Int).SetUint64(r.GasUsed)
		wantBalance.Add(wantBalance, fee.Mul(fee, r.EffectiveGasPrice))
	}
}

func testTraceTransactionRPCs(ctx context.Context, t *testing.T, sut *SUT, fb upgradechain.Block, eth *types.Block) {
	t.Helper()

	t.Run("debug_traceTransaction", func(t *testing.T) {
		receipts, err := sut.ethclient.BlockReceipts(ctx, rpc.BlockNumberOrHashWithHash(fb.Hash, false))
		require.NoError(t, err, "BlockReceipts(%s)", fb.Hash)
		require.Len(t, receipts, len(eth.Transactions()), "receipts per EVM transaction")

		for i, tx := range eth.Transactions() {
			var trace struct {
				From    common.Address `json:"from"`
				GasUsed hexutil.Uint64 `json:"gasUsed"`
			}
			err := sut.ethclient.Client().CallContext(ctx, &trace, "debug_traceTransaction", tx.Hash(), map[string]any{"tracer": "callTracer"})
			if nativeAssetCallTraceBroken(fb.Number) {
				require.EqualError(t, err, upgradechain.NativeAssetCallTraceError, "debug_traceTransaction(%s) matches coreth", tx.Hash())
				continue
			}
			require.NoError(t, err, "debug_traceTransaction(%s)", tx.Hash())
			assert.Equal(t, vmtest.TestEthAddrs[0], trace.From, "traced sender of %s", tx.Hash())

			// Pins coreth's gas accounting; notably the SSTORE refund counter
			// is disabled from Apricot Phase 1, which EIP-3529 replay
			// semantics would violate for the storage-clearing transaction.
			assert.Equal(t, receipts[i].GasUsed, uint64(trace.GasUsed), "traced gasUsed of %s matches the stored receipt", tx.Hash())
		}
	})
}

func testIntermediateRootsRPC(ctx context.Context, t *testing.T, sut *SUT, fb upgradechain.Block, eth *types.Block) {
	t.Helper()

	t.Run("debug_intermediateRoots", func(t *testing.T) {
		var roots []common.Hash
		require.NoError(t,
			sut.ethclient.Client().CallContext(ctx, &roots, "debug_intermediateRoots", fb.Hash),
			"debug_intermediateRoots(%s)", fb.Hash)
		require.Len(t, roots, len(eth.Transactions()), "one intermediate root per EVM transaction")

		// The roots exclude end-of-block operations, so the final root matches
		// the header only for blocks whose extData applies no atomic effects.
		if len(customtypes.BlockExtData(eth)) == 0 {
			assert.Equal(t, eth.Root(), roots[len(roots)-1], "final intermediate root is the block's state root")
		} else {
			assert.NotEqual(t, eth.Root(), roots[len(roots)-1],
				"atomic effects apply after the last transaction's intermediate root")
		}
	})
}

func testLogsRPC(ctx context.Context, t *testing.T, sut *SUT, fb upgradechain.Block) {
	t.Helper()

	if fb.Number != upgradechain.SendWarpMessageBlock {
		return
	}
	t.Run("eth_getLogs", func(t *testing.T) {
		blockHash := fb.Hash
		logs, err := sut.ethclient.FilterLogs(ctx, ethereum.FilterQuery{BlockHash: &blockHash})
		require.NoError(t, err, "FilterLogs(%s)", blockHash)
		assert.Len(t, logs, 1, "SendWarpMessage log")
	})
}
