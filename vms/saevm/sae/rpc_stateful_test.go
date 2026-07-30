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
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rlp"
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

// traceResult is [tracers.TxTraceResult] with Result typed by the tracer in
// use rather than `any`, which would JSON-unmarshal into a map.
type traceResult[T any] struct {
	TxHash common.Hash `json:"txHash"`
	Result T           `json:"result"`
	Error  string      `json:"error"`
}

// mutateBlockNonce re-seals the block with the given [types.Header] nonce,
// changing its hash. Execution ignores the nonce unless a hook reads it.
func mutateBlockNonce(b *types.Block, nonce uint64) *types.Block {
	hdr := b.Header()
	hdr.Nonce = types.EncodeNonce(nonce)
	return b.WithSeal(hdr)
}

// oneTxTrace returns the result the block-tracing methods return for a
// single-transaction block.
func oneTxTrace[T any](tx *types.Transaction, result T) []traceResult[T] {
	return []traceResult[T]{{
		TxHash: tx.Hash(),
		Result: result,
	}}
}

// blockRLP returns the block's RLP encoding, as accepted by debug_traceBlock.
func blockRLP(tb testing.TB, b *types.Block) hexutil.Bytes {
	tb.Helper()
	buf, err := rlp.EncodeToBytes(b)
	require.NoErrorf(tb, err, "rlp.EncodeToBytes(%T)", b)
	return buf
}

// blockRLPFile writes the block's RLP encoding to a temporary file, returning
// both the encoding and the file path, as accepted by debug_traceBlock and
// debug_traceBlockFromFile respectively.
func blockRLPFile(tb testing.TB, b *types.Block) (hexutil.Bytes, string) {
	tb.Helper()
	buf := blockRLP(tb, b)
	file := filepath.Join(tb.TempDir(), "block.rlp")
	require.NoError(tb, os.WriteFile(file, buf, 0o600), "os.WriteFile()")
	return buf, file
}

// onlyLOG1At returns cmp options for comparing the trace result of the LOG1
// opcode at pc in the provided code. It fails tb if code[pc] isn't LOG1.
//
// It ignores all other opcode results and ignores [logger.StructLogRes.Gas] and
// [logger.StructLogRes.GasCost].
func onlyLOG1At(tb testing.TB, code []byte, pc uint64) cmp.Options {
	tb.Helper()
	require.Equalf(tb, vm.LOG1, vm.OpCode(code[pc]), "Bad test setup; opcode at program counter %d", pc)
	return cmp.Options{
		cmpopts.IgnoreSliceElements(func(r logger.StructLogRes) bool {
			return r.Pc != pc || r.Op != vm.LOG1.String()
		}),
		cmpopts.IgnoreFields(logger.StructLogRes{}, "Gas", "GasCost"),
	}
}

// traceBlockTests returns an [rpcTest] for every method that traces a canonical
// block — by number, by hash, by RLP, and by RLP file — each of which MUST
// return want.
func traceBlockTests(tb testing.TB, b *types.Block, want any, extraCmpOpts ...cmp.Option) []rpcTest {
	tb.Helper()
	buf, file := blockRLPFile(tb, b)

	tests := []rpcTest{
		{
			method: "debug_traceBlockByNumber",
			args:   []any{hexutil.Uint64(b.NumberU64())},
		},
		{
			method: "debug_traceBlockByHash",
			args:   []any{b.Hash()},
		},
		{
			method: "debug_traceBlock",
			args:   []any{buf},
		},
		{
			method: "debug_traceBlockFromFile",
			args:   []any{file},
		},
	}
	for i := range tests {
		tests[i].want = want
	}
	return withCmpOpts(tests, extraCmpOpts...)
}

// traceFileStructLogs reads a file written by debug_standardTraceBlockToFile,
// returning its logs in the same form as the debug_trace* methods return them,
// so the same cmp options can be used.
func traceFileStructLogs(tb testing.TB, file string) []logger.StructLogRes {
	tb.Helper()

	trace, err := os.ReadFile(file)
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
	return logger.FormatLogs(steps)
}

// TestDebugTrace covers what the debug tracers return for canonical blocks:
// structured logs, call frames, and error paths.
func TestDebugTrace(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	deployBlock, escrowAddr, deployTx := sut.deployEscrow(t)

	const escrowDepositVal = 42
	recipient := common.Address{'r', 'e', 'c', 'v'}
	depositBlock, depositTx := sut.depositToEscrow(t, escrowAddr, recipient, big.NewInt(escrowDepositVal))

	// The full trace would be excessive and uninformative, so pin only the
	// LOG1 for `emit Deposit()`.
	const logPC = 185
	onlyLOG1 := onlyLOG1At(t, escrow.ByteCode(), logPC)

	want := []traceResult[*logger.ExecutionResult]{
		{
			TxHash: deployTx.Hash(),
			Result: &logger.ExecutionResult{
				Gas:         deployBlock.Receipts()[0].GasUsed,
				ReturnValue: common.Bytes2Hex(escrow.ByteCode()),
				StructLogs:  []logger.StructLogRes{},
			},
		},
		{
			TxHash: depositTx.Hash(),
			Result: &logger.ExecutionResult{
				Gas: depositBlock.Receipts()[0].GasUsed,
				StructLogs: []logger.StructLogRes{{
					Pc:    logPC,
					Op:    vm.LOG1.String(),
					Depth: 1,
					Stack: utils.PointerTo([]string{
						escrow.DepositEvent(recipient, uint256.NewInt(escrowDepositVal)).Topics[0].String(),
						"0x40", "0x80", // arbitrary memory locations selected by Solidity
					}),
				}},
			},
		},
	}
	wantDeploy, wantDeposit := want[:1], want[1:]

	tests := slices.Concat(
		traceBlockTests(t, deployBlock.EthBlock(), wantDeploy, onlyLOG1),
		traceBlockTests(t, depositBlock.EthBlock(), wantDeposit, onlyLOG1),
	)
	tests = append(tests, []rpcTest{
		{
			method:       "debug_traceBlockByNumber",
			args:         []any{rpc.LatestBlockNumber},
			want:         wantDeposit,
			extraCmpOpts: onlyLOG1,
		},
		{
			// The returned deposit balance proves that the call ran against the
			// post-execution state of the latest block.
			method: "debug_traceCall",
			args: []any{
				ethapi.TransactionArgs{
					To:   &escrowAddr,
					Data: utils.PointerTo(hexutil.Bytes(escrow.CallDataForBalance(recipient))),
				},
				rpc.LatestBlockNumber,
			},
			want: logger.ExecutionResult{
				ReturnValue: common.Bytes2Hex(uint256.NewInt(escrowDepositVal).PaddedBytes(32)),
			},
			extraCmpOpts: cmp.Options{
				cmpopts.IgnoreFields(logger.ExecutionResult{}, "Gas", "StructLogs"),
			},
		},
		{
			method:  "debug_traceTransaction",
			args:    []any{common.Hash{}},
			wantErr: testerr.Contains("not found"),
		},
		{
			method: "debug_traceTransaction",
			args: []any{depositTx.Hash(), tracers.TraceConfig{
				Tracer: utils.PointerTo(`{
					fault: function() {},
					result: function() {
						for (;;) {}
					}
				}`),
				Timeout: utils.PointerTo("10ms"),
			}},
			wantErr: testerr.Contains("execution timeout"),
		},
		{
			method: "debug_traceTransaction",
			args: []any{depositTx.Hash(), tracers.TraceConfig{
				Tracer: utils.PointerTo("callTracer"),
			}},
			want: native.CallFrame{
				From:    sut.wallet.Addresses()[0],
				Gas:     depositTx.Gas(),
				GasUsed: depositBlock.Receipts()[0].GasUsed,
				To:      &escrowAddr,
				Input:   escrow.CallDataToDeposit(recipient),
				Value:   big.NewInt(escrowDepositVal),
			},
			extraCmpOpts: cmp.Options{cmputils.BigInts()},
		},
	}...)

	for _, tx := range want {
		tests = append(tests, rpcTest{
			method:       "debug_traceTransaction",
			args:         []any{tx.TxHash},
			want:         *tx.Result,
			extraCmpOpts: onlyLOG1,
		})
	}

	sut.testRPC(ctx, t, tests...)
}

// TestDebugStandardTraceBlockToFile verifies the per-transaction
// structured-log files, named with the caller-named block's hash rather than
// the faked header's.
//
// Trace-file names contain random suffixes so [SUT.testRPC]'s comparison
// can't be used.
func TestDebugStandardTraceBlockToFile(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	code := saetest.LogTopOfStackAfter(saetest.Ops(vm.NUMBER))
	logPC := uint64(len(code) - 2) //#nosec G115 -- Known non-negative
	onlyLOG1 := onlyLOG1At(t, code, logPC)

	tx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		Gas:       1e6,
		GasFeeCap: big.NewInt(params.GWei),
		Data:      code,
	})
	b := sut.runConsensusLoop(t, tx)

	var files []string
	require.NoError(t, sut.CallContext(ctx, &files, "debug_standardTraceBlockToFile", b.Hash()), "CallContext(debug_standardTraceBlockToFile)")
	require.Len(t, files, 1, "one trace file per transaction")
	file := files[0]
	t.Cleanup(func() {
		assert.NoError(t, os.Remove(file), "os.Remove(trace file)")
	})

	// Blocks served for tracing carry faked headers, so the name demonstrates
	// that the canonical hash was reported instead of the block's own.
	assert.Containsf(t, filepath.Base(file), fmt.Sprintf("%#x", b.Hash().Bytes()[:4]), "file name returned by debug_standardTraceBlockToFile MUST contain the canonical hash of block %#x", b.Hash())

	// The contract LOG1s the block number, so this log only appears if the file
	// really does trace the execution, in the block that executed it.
	want := []logger.StructLogRes{{
		Pc:    logPC,
		Op:    vm.LOG1.String(),
		Depth: 1,
		Stack: utils.PointerTo([]string{
			uint256.NewInt(b.NumberU64()).Hex(),
			"0x0", "0x0", // LOG1's size and offset
		}),
	}}
	got := traceFileStructLogs(t, file)
	if diff := cmp.Diff(want, got, onlyLOG1); diff != "" {
		t.Errorf("Structured logs in file written by debug_standardTraceBlockToFile diff (-want +got):\n%s", diff)
	}
}

// TestDebugTraceExecutedBaseFee pins the base fee used when the debug APIs
// replay transactions, by tracing a contract that logs BASEFEE. The setup
// distinguishes the executed base fee from the parent's and the consensus
// header's, which a buggy replay might source instead.
func TestDebugTraceExecutedBaseFee(t *testing.T) {
	timeOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(t, 1, timeOpt, withGenesisBaseFee(params.GWei))

	code := saetest.LogTopOfStackAfter(saetest.Ops(vm.BASEFEE))
	logPC := uint64(len(code) - 2) //#nosec G115 -- Known non-negative
	onlyLOG1 := onlyLOG1At(t, code, logPC)

	newCreateTx := func() *types.Transaction {
		return sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			Gas:       1e6, // inflates the next block's worst-case fee bound
			GasFeeCap: big.NewInt(2 * params.GWei),
			Data:      code,
		})
	}

	parent := sut.runConsensusLoop(t, newCreateTx())
	vmTime.Advance(time.Second)
	b := sut.runConsensusLoop(t, newCreateTx())
	require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)

	// If these coincided with the executed fee, a replay sourcing the wrong
	// one would go undetected.
	baseFee := b.ExecutedBaseFee()
	require.NotZerof(t, baseFee.ToBig().Cmp(b.EthBlock().BaseFee()), "%T.ExecutedBaseFee() = consensus header's worst-case base fee (%v); fees MUST differ to be pinned", b, baseFee)
	require.NotZerof(t, baseFee.Cmp(parent.ExecutedBaseFee()), "%T.ExecutedBaseFee() = parent's executed base fee (%v); fees MUST differ to be pinned", b, baseFee)

	receipts := b.Receipts()
	require.Lenf(t, receipts, 1, "%T.Receipts()", b)
	txHash := b.Transactions()[0].Hash()

	want := logger.ExecutionResult{
		Gas: receipts[0].GasUsed,
		StructLogs: []logger.StructLogRes{{
			Pc:    logPC,
			Op:    vm.LOG1.String(),
			Depth: 1,
			Stack: utils.PointerTo([]string{
				baseFee.Hex(),
				"0x0", "0x0", // LOG1's size and offset
			}),
		}},
	}

	// debug_traceBlock accepts any block with a canonical parent, not just
	// blocks known to the backend. The nonce only has to differ from b's.
	ethBlock := b.EthBlock()
	nonCanonicalRLP := blockRLP(t, mutateBlockNonce(ethBlock, ethBlock.Nonce()+1))

	// All block-tracing methods MUST report the executed base fee.
	wantBlockTrace := oneTxTrace(b.Transactions()[0], &want)

	tests := traceBlockTests(t, ethBlock, wantBlockTrace, onlyLOG1)
	tests = append(tests, []rpcTest{
		{
			method:       "debug_traceTransaction",
			args:         []any{txHash},
			want:         want,
			extraCmpOpts: onlyLOG1,
		},
		{
			method:       "debug_traceBlock",
			args:         []any{nonCanonicalRLP},
			want:         wantBlockTrace,
			extraCmpOpts: onlyLOG1,
		},
	}...)

	sut.testRPC(ctx, t, tests...)
}

// TestDebugTraceReportedBlockHash pins the block hash a trace reports: the
// caller-named one, never the faked header's nor the canonical block's at that
// height.
func TestDebugTraceReportedBlockHash(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	newTransferTx := func() *types.Transaction {
		return sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &common.Address{'r', 'e', 'c', 'v'},
			Gas:       params.TxGas,
			GasFeeCap: big.NewInt(params.GWei),
			Value:     big.NewInt(1),
		})
	}

	canonicalTx := newTransferTx()
	canonical := sut.runConsensusLoop(t, canonicalTx).EthBlock()

	sibling := mutateBlockNonce(canonical, canonical.Nonce()+1)

	unacceptedTx := newTransferTx()
	unaccepted := unwrap(t, sut.buildAndParseBlock(t, sut.lastAcceptedBlock(t), unacceptedTx)).EthBlock()
	unacceptedRLP, unacceptedFile := blockRLPFile(t, unaccepted)

	onlyBlockHash := cmp.Options{
		cmp.Transformer("onlyBlockHash", func(f native.FlatCallFrame) *common.Hash {
			return f.BlockHash
		}),
	}
	wantBlockHash := func(tx *types.Transaction, hash common.Hash) []traceResult[[]native.FlatCallFrame] {
		return oneTxTrace(tx, []native.FlatCallFrame{{BlockHash: &hash}})
	}

	flatCallTracer := tracers.TraceConfig{Tracer: utils.PointerTo("flatCallTracer")}
	sut.testRPC(ctx, t, withCmpOpts([]rpcTest{
		{
			name:   "canonical_by_hash",
			method: "debug_traceBlockByHash",
			args:   []any{canonical.Hash(), flatCallTracer},
			want:   wantBlockHash(canonicalTx, canonical.Hash()),
		},
		{
			name:   "canonical_by_number",
			method: "debug_traceBlockByNumber",
			args:   []any{hexutil.Uint64(canonical.NumberU64()), flatCallTracer},
			want:   wantBlockHash(canonicalTx, canonical.Hash()),
		},
		{
			name:   "supplied_sibling_of_canonical",
			method: "debug_traceBlock",
			args:   []any{blockRLP(t, sibling), flatCallTracer},
			want:   wantBlockHash(canonicalTx, sibling.Hash()),
		},
		{
			name:   "supplied_unaccepted",
			method: "debug_traceBlock",
			args:   []any{unacceptedRLP, flatCallTracer},
			want:   wantBlockHash(unacceptedTx, unaccepted.Hash()),
		},
		{
			name:   "supplied_unaccepted_from_file",
			method: "debug_traceBlockFromFile",
			args:   []any{unacceptedFile, flatCallTracer},
			want:   wantBlockHash(unacceptedTx, unaccepted.Hash()),
		},
	}, onlyBlockHash)...)
}

// TestDebugIntermediateRoots verifies that debug_intermediateRoots returns one
// root per transaction, the last of which is the block's post-execution root.
// Mid-block roots are never persisted, so those are asserted by property rather
// than by exact value.
func TestDebugIntermediateRoots(t *testing.T) {
	const numAccounts = 2 // one transfer per account
	ctx, sut := newSUT(t, numAccounts)

	transfers := make([]*types.Transaction, numAccounts)
	for i := range transfers {
		transfers[i] = sut.wallet.SetNonceAndSign(t, i, &types.LegacyTx{
			To:       &common.Address{'x', 'f', 'e', 'r'},
			Gas:      params.TxGas,
			GasPrice: big.NewInt(1),
			Value:    big.NewInt(1),
		})
	}
	block := sut.runConsensusLoop(t, transfers...)
	require.Lenf(t, block.Transactions(), len(transfers), "%T.Transactions()", block)

	var roots []common.Hash
	require.NoError(t, sut.CallContext(ctx, &roots, "debug_intermediateRoots", block.Hash()), "CallContext(debug_intermediateRoots)")

	require.Len(t, roots, len(transfers), "one root per transaction")
	assert.NotEqual(t, roots[0], roots[1], "each transfer changes state (nonce and balances)")
	// This holds only because nothing modifies state after the last tx:
	// hookstest.Stub.AfterExecutingBlock is a no-op and there are no
	// end-of-block ops. Hooks that mutate post-transaction state (e.g.
	// the C-Chain's) would break this!!
	assert.Equal(t, block.PostExecutionStateRoot(), roots[len(roots)-1], "last root is the block's post-execution root")
}

// TestDebugTraceBeforeBlockState pins whose before-block hook changes a replay
// runs against: the traced block's own, not the canonical child's. The hook
// credits marker with 1+nonce, distinguishing a supplied block's credit.
func TestDebugTraceBeforeBlockState(t *testing.T) {
	const suppliedNonce = 7
	marker := common.Address{'m', 'a', 'r', 'k'}
	ctx, sut := newSUT(t, 1)
	sut.hooks.BeforeExecutingBlockFn = func(_ params.Rules, sdb *state.StateDB, _ *types.Header, b *types.Block) error {
		sdb.AddBalance(marker, uint256.NewInt(1+b.Nonce()))
		return nil
	}
	newMarkerTx := func() *types.Transaction {
		return sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
			To:       &marker,
			Gas:      params.TxGas,
			GasPrice: big.NewInt(1),
		})
	}

	b1 := sut.runConsensusLoop(t)
	b2 := sut.runConsensusLoop(t, newMarkerTx())
	b2Tx := b2.Transactions()[0]
	unaccepted := unwrap(t, sut.buildAndParseBlock(t, sut.lastAcceptedBlock(t), newMarkerTx())).EthBlock()
	unacceptedTx := unaccepted.Transactions()[0]

	// Compared as a decimal string so that an absent account diffs legibly.
	onlyMarkerBalance := cmp.Options{
		cmp.Transformer("onlyMarkerBalance", func(accounts map[common.Address]native.Account) string {
			if balance := accounts[marker].Balance; balance != nil {
				return balance.String()
			}
			return "absent from prestate"
		}),
	}
	wantBalance := func(balance int64) map[common.Address]native.Account {
		return map[common.Address]native.Account{marker: {Balance: big.NewInt(balance)}}
	}

	prestateTracer := tracers.TraceConfig{Tracer: utils.PointerTo("prestateTracer")}
	sut.testRPC(ctx, t, withCmpOpts([]rpcTest{
		{
			// The canonical path sources its base state from
			// tracerBackend.StateAtBlock: b1's credit plus b2's own.
			name:   "canonical_block",
			method: "debug_traceBlockByHash",
			args:   []any{b2.Hash(), prestateTracer},
			want:   oneTxTrace(b2Tx, wantBalance(2)),
		},
		{
			// debug_traceTransaction sources its state from
			// backend.StateAtTransaction instead.
			name:   "canonical_transaction",
			method: "debug_traceTransaction",
			args:   []any{b2Tx.Hash(), prestateTracer},
			want:   wantBalance(2),
		},
		{
			// State as of b1 holds b1's credit alone; 2 would mean b2's
			// before-block changes leaked in.
			name:   "state_as_of_block",
			method: "debug_traceCall",
			args: []any{
				ethapi.TransactionArgs{
					From: utils.PointerTo(sut.wallet.Addresses()[0]),
					To:   &marker,
				},
				rpc.BlockNumber(b1.NumberU64()), // #nosec G115 -- block heights are small
				prestateTracer,
			},
			want: wantBalance(1),
		},
		{
			// A supplied sibling of b2: the canonical child at that height
			// (b2, crediting 1) MUST NOT be the source.
			name:   "supplied_sibling_of_canonical",
			method: "debug_traceBlock",
			args:   []any{blockRLP(t, mutateBlockNonce(b2.EthBlock(), suppliedNonce)), prestateTracer},
			want:   oneTxTrace(b2Tx, wantBalance(1+1+suppliedNonce)),
		},
		{
			// A supplied block whose parent is the tip, so no canonical child
			// exists; its changes MUST still be applied.
			name:   "supplied_unaccepted",
			method: "debug_traceBlock",
			args:   []any{blockRLP(t, mutateBlockNonce(unaccepted, suppliedNonce)), prestateTracer},
			want:   oneTxTrace(unacceptedTx, wantBalance(2+1+suppliedNonce)),
		},
	}, onlyMarkerBalance)...)
}

func TestStatefulRPCs(t *testing.T) {
	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(t, 1, opt)

	_, escrowAddr, _ := sut.deployEscrow(t)

	const escrowDepositVal = 42
	recipient := common.Address{'r', 'e', 'c', 'v'}
	b, _ := sut.depositToEscrow(t, escrowAddr, recipient, big.NewInt(escrowDepositVal))
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

	_, escrowAddr, _ := sut.deployEscrow(t)

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
