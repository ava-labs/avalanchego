// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"os"
	"path/filepath"
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

func TestDebugTrace(t *testing.T) {
	ctx, sut := newSUT(t, 2)

	deployBlock, escrowAddr, deployTx := sut.deployEscrow(t)

	const escrowDepositVal = 42
	recipient := common.Address{'r', 'e', 'c', 'v'}
	depositBlock, depositTx := sut.depositToEscrow(t, escrowAddr, recipient, big.NewInt(escrowDepositVal))

	// Specifying the entire trace would be excessive and uninformative so we
	// select a precise location of an event associated with the deposit()
	// function on the contract.
	const logPC = 185
	require.Equal(t, vm.LOG1, vm.OpCode(escrow.ByteCode()[logPC]), "Bad test setup; program counter LOG1 for `emit Deposit()`")
	ignore := cmp.Options{
		cmpopts.IgnoreSliceElements(func(r logger.StructLogRes) bool {
			return r.Pc != logPC || r.Op != vm.LOG1.String()
		}),
		// Any precise amount of gas left at the time of OpCode execution would
		// be copy-pasted from the test output.
		cmpopts.IgnoreFields(logger.StructLogRes{}, "Gas", "GasCost"),
	}

	want := []struct {
		TxHash common.Hash             `json:"txHash"`
		Result *logger.ExecutionResult `json:"result"`
		Error  string                  `json:"error"`
	}{
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

	blockFile := filepath.Join(t.TempDir(), "block.rlp")
	blockRLP, err := rlp.EncodeToBytes(depositBlock.EthBlock())
	require.NoErrorf(t, err, "rlp.EncodeToBytes(%T)", depositBlock.EthBlock())
	require.NoError(t, os.WriteFile(blockFile, blockRLP, 0o600), "os.WriteFile()")

	tests := []rpcTest{
		{
			method:       "debug_traceBlockByNumber",
			args:         []any{hexutil.Uint64(deployBlock.NumberU64())},
			want:         wantDeploy,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceBlockByNumber",
			args:         []any{hexutil.Uint64(depositBlock.NumberU64())},
			want:         wantDeposit,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceBlockByNumber",
			args:         []any{rpc.LatestBlockNumber},
			want:         wantDeposit,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceBlockByHash",
			args:         []any{deployBlock.Hash()},
			want:         wantDeploy,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceBlockByHash",
			args:         []any{depositBlock.Hash()},
			want:         wantDeposit,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceBlock",
			args:         []any{hexutil.Bytes(blockRLP)},
			want:         wantDeposit,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceBlockFromFile",
			args:         []any{blockFile},
			want:         wantDeposit,
			extraCmpOpts: ignore,
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
			method: "debug_intermediateRoots",
			args:   []any{deployBlock.Hash()},
			want:   []common.Hash{deployBlock.PostExecutionStateRoot()},
		},
		{
			method: "debug_intermediateRoots",
			args:   []any{depositBlock.Hash()},
			want:   []common.Hash{depositBlock.PostExecutionStateRoot()},
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
		// TODO(JonathanOppenheimer): add a fee-sensitive case; these assert
		// only fee-insensitive fields, leaving the SAE-era replay base fee
		// unpinned.
		{
			method: "debug_traceTransaction",
			args:   []any{depositTx.Hash(), tracers.TraceConfig{Tracer: utils.PointerTo("callTracer")}},
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
	}

	for _, tx := range want {
		tests = append(tests, rpcTest{
			method:       "debug_traceTransaction",
			args:         []any{tx.TxHash},
			want:         *tx.Result,
			extraCmpOpts: ignore,
		})
	}

	sut.testRPC(ctx, t, tests...)

	// Mid-block roots are never persisted, so assert properties rather than
	// exact values.
	t.Run("debug_intermediateRoots_multiple_txs", func(t *testing.T) {
		transfers := make([]*types.Transaction, 2)
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
		assert.Equal(t, block.PostExecutionStateRoot(), roots[len(roots)-1], "last root is the block's post-execution root")
	})

	// Trace-file names contain random suffixes so [SUT.testRPC]'s comparison
	// can't be used.
	t.Run("debug_standardTraceBlockToFile", func(t *testing.T) {
		var files []string
		require.NoError(t, sut.CallContext(ctx, &files, "debug_standardTraceBlockToFile", depositBlock.Hash()), "CallContext(debug_standardTraceBlockToFile)")
		require.Len(t, files, 1, "one trace file per transaction")
		t.Cleanup(func() {
			assert.NoError(t, os.Remove(files[0]), "os.Remove(trace file)")
		})

		trace, err := os.ReadFile(files[0])
		require.NoErrorf(t, err, "os.ReadFile(%q)", files[0])
		// The file holds a struct log: one JSON line per EVM opcode executed.
		// `emit Deposit()` compiles to a LOG1 opcode, so if it shows up the
		// file really does trace the transaction's execution.
		assert.Contains(t, string(trace), vm.LOG1.String(), "trace of `emit Deposit()`")
	})
}

// TestDebugTraceBeforeBlockHook verifies that tracing a block applies the
// block's own before-block hook changes, while querying state as of a block
// does not apply the next block's.
func TestDebugTraceBeforeBlockHook(t *testing.T) {
	marker := common.Address{'m', 'a', 'r', 'k'}
	ctx, sut := newSUT(t, 1)
	sut.hooks.BeforeExecutingBlockFn = func(_ params.Rules, sdb *state.StateDB, _ *types.Header, _ *types.Block) error {
		sdb.AddBalance(marker, uint256.NewInt(1))
		return nil
	}

	b1 := sut.runConsensusLoop(t)
	b2 := sut.runConsensusLoop(t, sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &marker,
		Gas:      params.TxGas,
		GasPrice: big.NewInt(1),
	}))

	// b2's post-execution root includes the hook's credit, so re-execution
	// only reproduces it if the trace's base state includes the credit too.
	t.Run("block_tracing_applies_hook", func(t *testing.T) {
		var roots []common.Hash
		require.NoError(t, sut.CallContext(ctx, &roots, "debug_intermediateRoots", b2.Hash()), "CallContext(debug_intermediateRoots)")
		require.Len(t, roots, 1, "one root per transaction")
		assert.Equal(t, b2.PostExecutionStateRoot(), roots[0], "trace base state includes the before-block hook's changes")
	})

	// The hook credited marker once per block, so as of b1 its balance is 1;
	// 2 means b2's before-block changes leaked in.
	t.Run("traceCall_does_not_apply_child_hook", func(t *testing.T) {
		var prestate map[common.Address]native.Account
		err := sut.CallContext(ctx, &prestate, "debug_traceCall",
			ethapi.TransactionArgs{
				From: utils.PointerTo(sut.wallet.Addresses()[0]),
				To:   &marker,
			},
			rpc.BlockNumber(b1.NumberU64()), // #nosec G115 -- block heights are small
			tracers.TraceConfig{Tracer: utils.PointerTo("prestateTracer")},
		)
		require.NoError(t, err, "CallContext(debug_traceCall, prestateTracer)")
		require.Contains(t, prestate, marker, "prestate accounts")
		assert.Equal(t, int64(1), prestate[marker].Balance.Int64(), "marker balance as of b1")
	})
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
