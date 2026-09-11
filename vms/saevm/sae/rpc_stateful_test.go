// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/eth/tracers"
	"github.com/ava-labs/libevm/eth/tracers/logger"
	"github.com/ava-labs/libevm/eth/tracers/native"
	"github.com/ava-labs/libevm/ethclient/gethclient"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/escrow"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
	ethereum "github.com/ava-labs/libevm"
)

// TestStateQueryOnNonCanonicalBlock verifies that state-dependent RPC calls
// (e.g. eth_getBalance) on a verified-but-not-accepted in-memory block return
// [blocks.ErrNonCanonicalBlock], while non-state lookups return nil (not found).
func TestStateQueryOnNonCanonicalBlock(t *testing.T) {
	ctx, sut := newSUT(t, 1)
	b := unwrap(t, sut.createAndVerifyBlock(t, sut.lastAcceptedBlock(t)))

	sut.testRPC(ctx, t, []rpcTest{
		{
			method:  "eth_getBalance",
			args:    []any{sut.wallet.Addresses()[0], rpc.BlockNumberOrHashWithHash(b.Hash(), false)},
			wantErr: testerr.Contains(blocks.ErrNonCanonicalBlock.Error()),
		},
		{
			method: "eth_getBlockByHash",
			args:   []any{b.Hash(), false},
			want:   (*types.Header)(nil),
		},
	}...)
}

// TestStateQueryBlocksUntilExecuted verifies that state-dependent RPC calls on
// an accepted-but-unexecuted block will wait until execution completes,
// regardless of whether the block is addressed by hash or height.
func TestStateQueryBlocksUntilExecuted(t *testing.T) {
	blockingPrecompile := common.Address{'b', 'l', 'o', 'c', 'k'}
	precompileOpt, unblock := withBlockingPrecompile(blockingPrecompile)
	ctx, sut := newSUT(t, 2, precompileOpt)
	defer unblock()

	addr := sut.wallet.Addresses()[1]
	want, err := sut.BalanceAt(ctx, addr, nil)
	require.NoError(t, err, "%T.BalanceAt(latest)", sut.Client)

	b := sut.runConsensusLoop(t, sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &blockingPrecompile,
		Gas:      params.TxGas,
		GasPrice: big.NewInt(1),
	}))

	// Running in parallel allows the main test to unblock() after the tests are
	// started.
	sut.testRPC(ctx, t, []rpcTest{
		{
			method:   "eth_getBalance",
			args:     []any{addr, rpc.BlockNumberOrHashWithHash(b.Hash(), false)},
			want:     (*hexutil.Big)(want),
			parallel: true,
		},
		{
			method:   "eth_getBalance",
			args:     []any{addr, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(b.Number().Int64()))},
			want:     (*hexutil.Big)(want),
			parallel: true,
		},
	}...)
}

// traceResult is [tracers.TxTraceResult] with a typed Result.
type traceResult[T any] struct {
	TxHash common.Hash `json:"txHash"`
	Result T           `json:"result"`
	Error  string      `json:"error"`
}

// writeRLPToFile writes the value's RLP encoding to a temporary file, returning
// both the encoding and the file path.
func writeRLPToFile(tb testing.TB, v any) (hexutil.Bytes, string) {
	tb.Helper()
	b := encodeRLP(tb, v)
	file := filepath.Join(tb.TempDir(), "value.rlp")
	require.NoError(tb, os.WriteFile(file, b, 0o600), "os.WriteFile()")
	return b, file
}

// logTopOfStackAfter returns code with a LOG1 of the top of the stack appended
// by [saetest.LogTopOfStackAfter], the program counter of that LOG1, and cmp
// options for comparing its trace result.
//
// The options ignore all other opcode results and ignore
// [logger.StructLogRes.Gas] and [logger.StructLogRes.GasCost].
func logTopOfStackAfter(tb testing.TB, code []byte) ([]byte, uint64, cmp.Options) {
	tb.Helper()
	codeWithLog := saetest.LogTopOfStackAfter(code)
	logPC := uint64(len(codeWithLog) - 2) //#nosec G115 -- LogTopOfStackAfter appends 4 bytes
	require.Equalf(tb, vm.LOG1, vm.OpCode(codeWithLog[logPC]), "Bad test setup; opcode at program counter %d", logPC)
	return codeWithLog, logPC, cmp.Options{
		cmpopts.IgnoreSliceElements(func(r logger.StructLogRes) bool {
			return r.Pc != logPC || r.Op != vm.LOG1.String()
		}),
		cmpopts.IgnoreFields(logger.StructLogRes{}, "Gas", "GasCost"),
	}
}

// startExecutingBlockResult describes the start-executing-block hook's inputs.
// The hashes commit to every field of the parent header and block it receives,
// so a faked or re-sealed one changes them. Balance counts the calls, which the
// hashes can't, as a hook applied twice records the same ones.
type startExecutingBlockResult struct {
	ParentHash common.Hash
	BlockHash  common.Hash
	Balance    *uint256.Int
}

func (r startExecutingBlockResult) Bytes() []byte {
	balance := r.Balance.Bytes32()
	return slices.Concat(
		r.ParentHash[:],
		r.BlockHash[:],
		balance[:],
	)
}

// Hex is the encoding of [startExecutingBlockResult.Bytes] that a caller sees
// in [logger.ExecutionResult.ReturnValue].
func (r startExecutingBlockResult) Hex() string {
	return common.Bytes2Hex(r.Bytes())
}

// withStartExecutingBlockPrecompile records each start-executing-block hook
// call in the precompile's own account, and registers a precompile returning
// the recorded [startExecutingBlockResult]. No debug_trace* endpoint exposes
// the hook, so returning its inputs as data is what makes them assertable.
func withStartExecutingBlockPrecompile(precompile common.Address) sutOption {
	var (
		parentHashSlot = common.Hash{0}
		blockHashSlot  = common.Hash{1}
	)
	precompileOpt := withPrecompile(precompile, vm.NewStatefulPrecompile(
		func(env vm.PrecompileEnvironment, _ []byte) ([]byte, error) {
			sdb := env.ReadOnlyState()
			result := startExecutingBlockResult{
				ParentHash: sdb.GetState(precompile, parentHashSlot),
				BlockHash:  sdb.GetState(precompile, blockHashSlot),
				Balance:    sdb.GetBalance(precompile),
			}
			return result.Bytes(), nil
		},
	))

	return options.Func[sutConfig](func(c *sutConfig) {
		precompileOpt.Configure(c)
		c.hooks.StartExecutingBlockFn = func(_ params.Rules, sdb *state.StateDB, parent *types.Header, b *types.Block) error {
			sdb.SetState(precompile, parentHashSlot, parent.Hash())
			sdb.SetState(precompile, blockHashSlot, b.Hash())
			// A non-empty account stops EIP-158 deleting the slots above, as
			// emptiness ignores storage. Also acts as a counter for the number
			// of times the hook is called.
			sdb.AddBalance(precompile, uint256.NewInt(1))
			return nil
		}
	})
}

// TestDebugTrace covers the debug namespace's tracing endpoints. Blocks execute
// after acceptance, so every endpoint replays against a faked header carrying
// post-execution results.
func TestDebugTrace(t *testing.T) {
	// A controlled clock keeps the executed base fees reproducible.
	timeOpt, clock := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	precompile := common.Address{'p', 'r', 'e', 'c', 'o', 'm', 'p'}
	ctx, sut := newSUT(t, 2,
		timeOpt,
		// Using an increased base fee allows the fee to change during the test.
		withGenesisBaseFee(params.GWei),
		withStartExecutingBlockPrecompile(precompile),
	)
	var (
		sender = sut.wallet.Addresses()[0]
		// Above the genesis base fee, which rises as the blocks below burn gas.
		gasPrice = big.NewInt(2 * params.GWei)
	)

	// Issuing two blocks with over-declared gas ensures that the parent's
	// worstcase and executed base fees differ.
	burnGas := func() *types.Transaction {
		return sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			GasFeeCap: gasPrice,
			Gas:       1e6,
		})
	}
	sut.runConsensusLoop(t, burnGas())
	parent := sut.runConsensusLoop(t, burnGas())
	// The parent block's base fees MUST differ to ensure we are asserting that
	// the correct value is used.
	require.NotZero(t, parent.EthBlock().BaseFee().Cmp(parent.ExecutedBaseFee().ToBig()), "Worst-case and executed base fees MUST differ")

	callPrecompile := func() *types.Transaction {
		return sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			GasFeeCap: gasPrice,
			Gas:       1e6,
			To:        &precompile,
		})
	}

	// Time passing between the parent and the traced block means the gas clock
	// MUST be fast-forwarded to the traced block's time to reach its base fee.
	clock.Advance(time.Second)

	// The traced block reports what its start-executing-block hook saw at index
	// 0 and logs the base fee it replayed with at index 1.
	precompileTx := callPrecompile()
	logBaseFeeCode, logBaseFeePC, cmpBaseFeeLOG1 := logTopOfStackAfter(t, saetest.Ops(vm.BASEFEE))
	baseFeeTx := sut.wallet.SetNonceAndSign(t, 1, &types.DynamicFeeTx{
		GasFeeCap: gasPrice,
		Gas:       1e6,
		Data:      logBaseFeeCode,
	})
	b := sut.runConsensusLoop(t, precompileTx, baseFeeTx)
	require.NotEqual(t, parent.BuildTime(), b.BuildTime(), "Parent and traced block build times MUST differ")

	require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	require.Lenf(t, b.Transactions(), 2, "%T.Transactions()", b)

	baseFee := b.ExecutedBaseFee()
	// The block's base fees MUST differ to ensure we are asserting that the
	// correct value is used.
	require.NotZero(t, b.EthBlock().BaseFee().Cmp(baseFee.ToBig()), "Worst-case and executed base fees MUST differ")

	ethBlock := b.EthBlock()
	blockRLP, blockFile := writeRLPToFile(t, ethBlock)

	// Sibling was rejected in favor of b, but can still be traced.
	siblingHeader := ethBlock.Header()
	siblingHeader.Nonce = types.EncodeNonce(ethBlock.Nonce() + 1)
	sibling := ethBlock.WithSeal(siblingHeader)

	unacceptedTx := callPrecompile()
	unaccepted := unwrap(t, sut.buildAndParseBlock(t, sut.lastAcceptedBlock(t), unacceptedTx)).EthBlock()
	unacceptedRLP, unacceptedFile := writeRLPToFile(t, unaccepted)

	// precompileResult is what the precompile should return during the provided
	// block's execution.
	precompileResult := func(block *types.Block) startExecutingBlockResult {
		return startExecutingBlockResult{
			ParentHash: block.ParentHash(),
			BlockHash:  block.Hash(),
			Balance:    uint256.NewInt(block.NumberU64()),
		}
	}
	// wantPrecompileResults returns the results of tracing the block, whose
	// first transaction calls the precompile and whose others return nothing.
	wantPrecompileResults := func(block *types.Block) []traceResult[*logger.ExecutionResult] {
		txs := block.Transactions()
		results := make([]traceResult[*logger.ExecutionResult], len(txs))
		for i, tx := range txs {
			results[i] = traceResult[*logger.ExecutionResult]{
				TxHash: tx.Hash(),
				Result: new(logger.ExecutionResult),
			}
		}
		results[0].Result.ReturnValue = precompileResult(block).Hex()
		return results
	}
	wantTracedResults := wantPrecompileResults(ethBlock)
	callPrecompileArgs := ethapi.TransactionArgs{
		From: utils.PointerTo(sender),
		To:   &precompile,
	}

	t.Run("before_block_hook", func(t *testing.T) {
		sut.testRPC(ctx, t, withCmpOpts(
			[]rpcTest{
				{
					method: "debug_traceBlockByNumber",
					args:   []any{hexutil.Uint64(ethBlock.NumberU64())},
					want:   wantTracedResults,
				},
				{
					name:   "latest_block",
					method: "debug_traceBlockByNumber",
					args:   []any{rpc.LatestBlockNumber},
					want:   wantTracedResults,
				},
				{
					method: "debug_traceBlockByHash",
					args:   []any{ethBlock.Hash()},
					want:   wantTracedResults,
				},
				{
					// Tracing by RLP MUST match tracing by number, so the fee the
					// block is re-sealed with cannot reach the hook.
					method: "debug_traceBlock",
					args:   []any{blockRLP},
					want:   wantTracedResults,
				},
				{
					method: "debug_traceBlockFromFile",
					args:   []any{blockFile},
					want:   wantTracedResults,
				},
				{
					method: "debug_traceTransaction",
					args:   []any{precompileTx.Hash()},
					want: logger.ExecutionResult{
						ReturnValue: precompileResult(ethBlock).Hex(),
					},
				},
				{
					// The supplied block reaches the hook, not the canonical
					// sibling at the same height.
					name:   "supplied_sibling_of_canonical",
					method: "debug_traceBlock",
					args:   []any{encodeRLP(t, sibling)},
					want:   wantPrecompileResults(sibling),
				},
				{
					name:   "supplied_unaccepted",
					method: "debug_traceBlock",
					args:   []any{unacceptedRLP},
					want:   wantPrecompileResults(unaccepted),
				},
				{
					name:   "supplied_unaccepted_from_file",
					method: "debug_traceBlockFromFile",
					args:   []any{unacceptedFile},
					want:   wantPrecompileResults(unaccepted),
				},
				{
					name:   "call_on_latest",
					method: "debug_traceCall",
					args:   []any{callPrecompileArgs, rpc.LatestBlockNumber},
					want: logger.ExecutionResult{
						ReturnValue: precompileResult(ethBlock).Hex(),
					},
				},
				{
					// debug_traceCall applies no start-executing-block changes, so
					// a result carrying the canonical child's would mean they
					// leaked in.
					name:   "call_on_parent",
					method: "debug_traceCall",
					args:   []any{callPrecompileArgs, rpc.BlockNumber(parent.NumberU64())}, // #nosec G115 -- block heights are small
					want: logger.ExecutionResult{
						ReturnValue: precompileResult(parent.EthBlock()).Hex(),
					},
				},
			},
			// Gas and structured logs are the base-fee subtest's concern.
			cmpopts.IgnoreFields(logger.ExecutionResult{}, "Gas", "StructLogs"),
		)...)
	})

	wantBaseFeeTxResult := logger.ExecutionResult{
		Gas: b.Receipts()[1].GasUsed,
		StructLogs: []logger.StructLogRes{{
			Pc:    logBaseFeePC,
			Op:    vm.LOG1.String(),
			Depth: 1,
			Stack: utils.PointerTo([]string{
				baseFee.Hex(),
				"0x0", "0x0", // LOG1's size and offset
			}),
		}},
	}
	wantBaseFeeBlockResults := []traceResult[*logger.ExecutionResult]{
		{
			TxHash: precompileTx.Hash(),
			Result: &logger.ExecutionResult{
				Gas: b.Receipts()[0].GasUsed,
			},
		},
		{
			TxHash: baseFeeTx.Hash(),
			Result: &wantBaseFeeTxResult,
		},
	}

	t.Run("executed_base_fee", func(t *testing.T) {
		sut.testRPC(ctx, t, withCmpOpts(
			[]rpcTest{
				{
					method: "debug_traceBlockByNumber",
					args:   []any{hexutil.Uint64(ethBlock.NumberU64())},
					want:   wantBaseFeeBlockResults,
				},
				{
					name:   "latest_block",
					method: "debug_traceBlockByNumber",
					args:   []any{rpc.LatestBlockNumber},
					want:   wantBaseFeeBlockResults,
				},
				{
					method: "debug_traceBlockByHash",
					args:   []any{ethBlock.Hash()},
					want:   wantBaseFeeBlockResults,
				},
				{
					// The supplied header carries the worst-case bound, so the
					// executed fee in the result proves it was discarded.
					method: "debug_traceBlock",
					args:   []any{blockRLP},
					want:   wantBaseFeeBlockResults,
				},
				{
					method: "debug_traceBlockFromFile",
					args:   []any{blockFile},
					want:   wantBaseFeeBlockResults,
				},
				{
					method: "debug_traceTransaction",
					args:   []any{baseFeeTx.Hash()},
					want:   wantBaseFeeTxResult,
				},
				{
					name:   "call_on_latest",
					method: "debug_traceCall",
					args: []any{
						ethapi.TransactionArgs{
							From: utils.PointerTo(sender),
							Data: utils.PointerTo(hexutil.Bytes(logBaseFeeCode)),
							// Traced calls set [vm.Config.NoBaseFee], which
							// zeroes the base fee unless we pay a gas price.
							GasPrice: (*hexutil.Big)(gasPrice),
						},
						rpc.LatestBlockNumber,
					},
					want: logger.ExecutionResult{
						StructLogs: wantBaseFeeTxResult.StructLogs,
					},
					// A call consumes different gas to the transaction above.
					extraCmpOpts: cmp.Options{
						cmpopts.IgnoreFields(logger.ExecutionResult{}, "Gas"),
					},
				},
			},
			cmpBaseFeeLOG1,
			// Return values belong to the hook subtest, and the precompile's
			// transaction has no structured logs to compare.
			cmpopts.IgnoreFields(logger.ExecutionResult{}, "ReturnValue"),
			cmpopts.EquateEmpty(),
		)...)
	})

	// wantBlockHash returns the results of tracing the block, ech frame should
	// report the block's own hash.
	wantBlockHash := func(block *types.Block) []traceResult[[]native.FlatCallFrame] {
		hash := block.Hash()
		txs := block.Transactions()
		results := make([]traceResult[[]native.FlatCallFrame], len(txs))
		for i, tx := range txs {
			results[i] = traceResult[[]native.FlatCallFrame]{
				TxHash: tx.Hash(),
				Result: []native.FlatCallFrame{
					{BlockHash: &hash},
				},
			}
		}
		return results
	}
	flatCallTracer := tracers.TraceConfig{
		Tracer: utils.PointerTo("flatCallTracer"),
	}

	t.Run("reported_block_hash", func(t *testing.T) {
		sut.testRPC(ctx, t, withCmpOpts(
			[]rpcTest{
				{
					name:   "canonical_by_hash",
					method: "debug_traceBlockByHash",
					args:   []any{ethBlock.Hash(), flatCallTracer},
					want:   wantBlockHash(ethBlock),
				},
				{
					name:   "canonical_by_number",
					method: "debug_traceBlockByNumber",
					args:   []any{hexutil.Uint64(ethBlock.NumberU64()), flatCallTracer},
					want:   wantBlockHash(ethBlock),
				},
				{
					name:   "supplied_sibling_of_canonical",
					method: "debug_traceBlock",
					args:   []any{encodeRLP(t, sibling), flatCallTracer},
					want:   wantBlockHash(sibling),
				},
				{
					name:   "supplied_unaccepted",
					method: "debug_traceBlock",
					args:   []any{unacceptedRLP, flatCallTracer},
					want:   wantBlockHash(unaccepted),
				},
				{
					name:   "supplied_unaccepted_from_file",
					method: "debug_traceBlockFromFile",
					args:   []any{unacceptedFile, flatCallTracer},
					want:   wantBlockHash(unaccepted),
				},
			},
			cmp.Transformer("onlyBlockHash", func(f native.FlatCallFrame) *common.Hash {
				return f.BlockHash
			}),
		)...)
	})

	// TODO(StephenButtolph): convert these to e2e tests, which exercise the
	// built binary. This file imports the native tracer package for its own
	// types, so it registers "callTracer" itself and would pass were the rpc
	// package's force-load deleted. Nothing here imports the JavaScript
	// evaluator, so that row does depend on the force-load, but is fragile.
	t.Run("named_tracers", func(t *testing.T) {
		sut.testRPC(ctx, t, []rpcTest{
			{
				name:   "call_tracer",
				method: "debug_traceTransaction",
				args: []any{precompileTx.Hash(), tracers.TraceConfig{
					Tracer: utils.PointerTo("callTracer"),
				}},
				want: native.CallFrame{
					From:    sender,
					To:      &precompile,
					Gas:     precompileTx.Gas(),
					GasUsed: b.Receipts()[0].GasUsed,
					Value:   big.NewInt(0),
				},
				extraCmpOpts: cmp.Options{
					cmputils.BigInts(),
					// Output belongs to the hook subtest.
					cmpopts.IgnoreFields(native.CallFrame{}, "Output"),
				},
			},
			{
				name:   "javascript",
				method: "debug_traceTransaction",
				args: []any{precompileTx.Hash(), tracers.TraceConfig{
					Tracer: utils.PointerTo(`{
						fault: function() {},
						result: function() { return "ok" }
					}`),
				}},
				want: "ok",
			},
		}...)
	})
}

// readStructLogsFromFile decodes the file's stream of JSON-encoded
// [logger.StructLog] values.
func readStructLogsFromFile(tb testing.TB, file string) []logger.StructLog {
	tb.Helper()

	trace, err := os.ReadFile(file) //#nosec G304 -- The path comes from the test itself, not from user input.
	require.NoErrorf(tb, err, "os.ReadFile(%q)", file)

	// Every line describes one executed opcode, bar the last, which summarises
	// the execution and therefore decodes as a zero [logger.StructLog].
	var steps []logger.StructLog
	dec := json.NewDecoder(bytes.NewReader(trace))
	for dec.More() {
		var step logger.StructLog
		require.NoError(tb, dec.Decode(&step), "decoding trace line")
		steps = append(steps, step)
	}
	return steps
}

// TestDebugStandardTraceBlockToFile verifies the per-transaction structured-log
// files, named with the caller-named block's hash rather than the faked
// header's.
//
// This function returns random output and produces files as a side-effect, so
// [SUT.testRPC]'s comparison can't be used.
func TestDebugStandardTraceBlockToFile(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	code, logPC, onlyLOG1 := logTopOfStackAfter(t, saetest.Ops(vm.NUMBER))
	tx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		Gas:       1e6,
		GasFeeCap: big.NewInt(params.GWei),
		Data:      code,
	})
	b := sut.runConsensusLoop(t, tx)

	var files []string
	require.NoError(t, sut.CallContext(ctx, &files, "debug_standardTraceBlockToFile", b.Hash()), "CallContext(debug_standardTraceBlockToFile)")
	t.Cleanup(func() {
		for _, file := range files {
			assert.NoErrorf(t, os.Remove(file), "os.Remove(trace file: %q)", file)
		}
	})

	require.Len(t, files, 1, "one trace file per transaction")
	file := files[0]

	// Blocks served for tracing carry faked headers, so the name demonstrates
	// that the real hash was reported instead of the faked hash own.
	hashInFileName := fmt.Sprintf("%#x", b.Hash().Bytes()[:4])
	assert.Containsf(t, file, hashInFileName, "file name returned by debug_standardTraceBlockToFile MUST contain the canonical hash of block %#x", b.Hash())

	// The contract LOG1s the block number, so this log only appears if the file
	// really does trace the execution in the block that executed it.
	want := []logger.StructLogRes{{
		Pc:    logPC,
		Op:    vm.LOG1.String(),
		Depth: 1,
		Stack: utils.PointerTo([]string{
			uint256.NewInt(b.NumberU64()).Hex(),
			"0x0", "0x0", // LOG1's size and offset
		}),
	}}
	got := logger.FormatLogs(readStructLogsFromFile(t, file))
	if diff := cmp.Diff(want, got, onlyLOG1); diff != "" {
		t.Errorf("Structured logs in file written by debug_standardTraceBlockToFile diff (-want +got):\n%s", diff)
	}
}

// TestDebugIntermediateRoots verifies that debug_intermediateRoots returns one
// root per transaction, the last of which is the block's post-execution root.
func TestDebugIntermediateRoots(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	const numTxs = 2
	txs := make([]*types.Transaction, numTxs)
	for i := range txs {
		txs[i] = sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       &common.Address{},
			Gas:      params.TxGas,
			GasPrice: big.NewInt(1),
			Value:    big.NewInt(1),
		})
	}
	block := sut.runConsensusLoop(t, txs...)
	require.Lenf(t, block.Transactions(), len(txs), "%T.Transactions()", block)

	var roots []common.Hash
	require.NoError(t, sut.CallContext(ctx, &roots, "debug_intermediateRoots", block.Hash()), "CallContext(debug_intermediateRoots)")

	require.Len(t, roots, numTxs, "one root per transaction")
	assert.NotEqual(t, roots[0], roots[1], "each transfer changes state (nonce and balances)")
	// This holds only because nothing modifies state after the last tx:
	// hookstest.Stub.FinishExecutingBlock is a no-op and there are no
	// end-of-block ops. Hooks that mutate post-transaction state (e.g.
	// the C-Chain's) would break this!!
	assert.Equal(t, block.PostExecutionStateRoot(), roots[numTxs-1], "last root is the block's post-execution root")
}

func TestStatefulRPCs(t *testing.T) {
	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(t, 1, opt)

	escrowAddr := sut.deployEscrow(t)

	const escrowDepositVal = 42
	recipient := common.Address{'r', 'e', 'c', 'v'}
	b := sut.depositToEscrow(t, escrowAddr, recipient, big.NewInt(escrowDepositVal))
	callMsg := ethereum.CallMsg{
		From: sut.wallet.Addresses()[0],
		To:   &escrowAddr,
		Data: escrow.CallDataForBalance(recipient),
	}

	vmTime.AdvanceToSettle(ctx, t, b)
	for range 2 {
		bb := sut.runConsensusLoop(t)
		vmTime.AdvanceToSettle(ctx, t, bb)
	}
	_, ok := sut.rawVM.consensusCritical.Load(b.Hash())
	require.Falsef(t, ok, "%T[%#x] still in VM memory", b, b.Hash())

	storageKey := escrow.StorageKeyForBalance(recipient)
	storageKeyHex := storageKey.Hex()

	gc := gethclient.New(sut.rpcClient)

	wantStorageValue := big.NewInt(escrowDepositVal)
	wantStorageBytes := uint256.NewInt(escrowDepositVal).PaddedBytes(32)

	tests := []struct {
		name string
		num  rpc.BlockNumber
	}{
		{
			name: "block_in_memory",
			num:  rpc.LatestBlockNumber,
		},
		{
			name: "block_on_disk",
			num:  rpc.BlockNumber(b.Number().Int64()),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blockNum := big.NewInt(int64(tt.num))

			t.Run("eth_call", func(t *testing.T) {
				got, err := sut.CallContract(ctx, callMsg, blockNum)
				require.NoError(t, err, "CallContract()")
				assert.Equal(t, wantStorageBytes, got, "CallContract() result")
			})

			t.Run("eth_getBalance", func(t *testing.T) {
				got, err := sut.BalanceAt(ctx, escrowAddr, blockNum)
				require.NoError(t, err, "BalanceAt()")
				require.Zero(t, wantStorageValue.Cmp(got), "BalanceAt(): want %d, got %s", wantStorageValue, got)
			})

			t.Run("eth_getCode", func(t *testing.T) {
				got, err := sut.CodeAt(ctx, escrowAddr, blockNum)
				require.NoError(t, err, "CodeAt()")
				assert.Equal(t, escrow.ByteCode(), got, "CodeAt() result")
			})

			t.Run("eth_getStorageAt", func(t *testing.T) {
				got, err := sut.StorageAt(ctx, escrowAddr, storageKey, blockNum)
				require.NoError(t, err, "StorageAt()")
				assert.Equal(t, wantStorageBytes, got, "StorageAt() result")
			})

			t.Run("eth_getProof", func(t *testing.T) {
				got, err := gc.GetProof(ctx, escrowAddr, []string{storageKeyHex}, blockNum)
				require.NoError(t, err, "GetProof()")
				require.NotNil(t, got, "GetProof() result")

				saetest.VerifyProof(t, b.PostExecutionStateRoot(), got)
				assert.Equal(t, escrowAddr, got.Address, "GetProof().Address")
				assert.Zerof(t, wantStorageValue.Cmp(got.Balance), "GetProof().Balance: want %d, got %s", wantStorageValue, got.Balance)
				assert.Equal(t, crypto.Keccak256Hash(escrow.ByteCode()), got.CodeHash, "GetProof().CodeHash")
				assert.Equal(t, uint64(1), got.Nonce, "GetProof().Nonce")

				require.Len(t, got.StorageProof, 1, "len(GetProof().StorageProof)")
				storage := got.StorageProof[0]
				assert.Equal(t, storageKeyHex, storage.Key, "GetProof().StorageProof[0].Key")
				assert.Zerof(t, wantStorageValue.Cmp(storage.Value), "GetProof().StorageProof[0].Value: want %d, got %s", wantStorageValue, storage.Value)
			})
		})
	}
}

// TestStatefulRPCsLatestOnly tests stateful RPC methods that don't accept a
// block number parameter via ethclient/gethclient and so always run against
// the latest block: eth_estimateGas and eth_createAccessList.
func TestStatefulRPCsLatestOnly(t *testing.T) {
	ctx, sut := newSUT(t, 1)
	gc := gethclient.New(sut.rpcClient)

	escrowAddr := sut.deployEscrow(t)

	recipient := common.Address{'r', 'e', 'c', 'v'}
	callMsg := ethereum.CallMsg{
		From: sut.wallet.Addresses()[0],
		To:   &escrowAddr,
		Data: escrow.CallDataForBalance(recipient),
	}
	requireCallSucceedsWithGas := func(t *testing.T, msg ethereum.CallMsg, gas uint64) {
		t.Helper()

		msg.Gas = gas
		_, err := sut.CallContract(ctx, msg, nil)
		require.NoErrorf(t, err, "CallContract() with gas %d", gas)
	}

	t.Run("eth_estimateGas", func(t *testing.T) {
		gas, err := sut.EstimateGas(ctx, callMsg)
		require.NoError(t, err, "EstimateGas()")
		requireCallSucceedsWithGas(t, callMsg, gas)
	})

	t.Run("eth_createAccessList", func(t *testing.T) {
		accessList, gas, errMsg, err := gc.CreateAccessList(ctx, callMsg)
		require.NoError(t, err, "CreateAccessList()")
		require.Empty(t, errMsg, "CreateAccessList() error message")

		wantAccessList := &types.AccessList{{
			Address:     escrowAddr,
			StorageKeys: []common.Hash{escrow.StorageKeyForBalance(recipient)},
		}}
		require.Equal(t, wantAccessList, accessList, "CreateAccessList() access list")

		msg := callMsg
		msg.AccessList = *accessList
		requireCallSucceedsWithGas(t, msg, gas)
	})
}

func TestContractBindingsWhenPendingResolvesToLastExecuted(t *testing.T) {
	blocking := common.Address{'b', 'l', 'o', 'c', 'k'}
	opt, unblock := withBlockingPrecompile(blocking)
	defer unblock()

	ctx, sut := newSUT(
		t, 3, opt,
		options.Func[sutConfig](func(c *sutConfig) {
			// The [bind] package makes extensive use of [rpc.PendingBlockNumber],
			// which breaks when resolved as the last-accepted block.
			c.vmConfig.RPCConfig.ResolvePendingToLastExecuted = true
		}),
	)

	chainID, err := sut.ChainID(ctx)
	require.NoErrorf(t, err, "%T.ChainID()", sut.Client)
	opts, err := bind.NewKeyedTransactorWithChainID(sut.wallet.PrivateKey(0), chainID)
	require.NoError(t, err, "bind.NewKeyedTransactorWithChainID(...)")

	addr := sut.deployEscrow(t)
	contract := bind.NewBoundContract(addr, escrow.ABI(t), sut.Client, sut.Client, sut.Client)

	deposit := uint256.NewInt(42)
	recipient := sut.wallet.Addresses()[1]
	opts.Value = deposit.ToBig()
	tx, err := contract.Transact(opts, "deposit", recipient)
	require.NoErrorf(t, err, "%T.Transact(..., %q, %v)", contract, "deposit", recipient)

	sut.waitUntilTxsPending(t, tx)
	b := sut.runConsensusLoop(t)

	// No need to wait until executed! #LiveReceipts

	sut.testRPC(ctx, t, rpcTest{
		method: "eth_getTransactionReceipt",
		args:   []any{tx.Hash()},
		want: &types.Receipt{
			Type:        tx.Type(),
			Status:      types.ReceiptStatusSuccessful,
			BlockHash:   b.Hash(),
			BlockNumber: b.Number(),
			TxHash:      tx.Hash(),
			Logs: []*types.Log{escrow.WithDepositTopicsAndData(
				&types.Log{
					Address:     addr,
					BlockNumber: b.NumberU64(),
					BlockHash:   b.Hash(),
					TxHash:      tx.Hash(),
				},
				recipient,
				deposit,
			)},
		},
		extraCmpOpts: []cmp.Option{
			cmpopts.IgnoreFields(
				types.Receipt{},
				"Bloom",
				"EffectiveGasPrice",
				"CumulativeGasUsed",
				"GasUsed",
			),
		},
	})

	t.Run("pending_resolves_to_last_executed", func(t *testing.T) {
		sut.runConsensusLoop(t, sut.wallet.SetNonceAndSign(t, 2, &types.LegacyTx{
			// The blocking precompile stops this transaction from completing,
			// ensuring that the block doesn't become the last-executed. It
			// does, however, increment the last-accepted block thus ensuring
			// that the test doesn't pass erroneously by having
			// accepted==executed.
			To:       &blocking,
			GasPrice: big.NewInt(1),
			Gas:      1e6,
		}))

		sut.testRPC(ctx, t, rpcTest{
			method: "eth_getHeaderByNumber",
			args:   []any{rpc.PendingBlockNumber},
			want:   b.Header(),
		})
	})
}
