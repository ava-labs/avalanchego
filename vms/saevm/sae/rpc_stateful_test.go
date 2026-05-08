// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/eth/tracers/logger"
	"github.com/ava-labs/libevm/ethclient/gethclient"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest/escrow"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
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
	ctx, sut := newSUT(t, 1)

	const escrowDepositVal = 42
	deployBlock, depositBlock, _, recv, _ := sut.deployEscrow(t, big.NewInt(escrowDepositVal))
	deployTxHash := deployBlock.Transactions()[0].Hash()
	depositTxHash := depositBlock.Transactions()[0].Hash()

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
			TxHash: deployTxHash,
			Result: &logger.ExecutionResult{
				Gas:         deployBlock.Receipts()[0].GasUsed,
				ReturnValue: common.Bytes2Hex(escrow.ByteCode()),
				StructLogs:  []logger.StructLogRes{},
			},
		},
		{
			TxHash: depositTxHash,
			Result: &logger.ExecutionResult{
				Gas: depositBlock.Receipts()[0].GasUsed,
				StructLogs: []logger.StructLogRes{{
					Pc:    logPC,
					Op:    vm.LOG1.String(),
					Depth: 1,
					Stack: utils.PointerTo([]string{
						escrow.DepositEvent(recv, uint256.NewInt(escrowDepositVal)).Topics[0].String(),
						"0x40", "0x80", // arbitrary memory locations selected by Solidity
					}),
				}},
			},
		},
	}
	wantDeploy, wantDeposit := want[:1], want[1:]

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
			method:  "debug_traceTransaction",
			args:    []any{common.Hash{}},
			wantErr: testerr.Contains("not found"),
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
}

func TestStatefulRPCs(t *testing.T) {
	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))
	ctx, sut := newSUT(t, 1, opt)

	const escrowDepositVal = 42
	_, b, escrowAddr, recv, callMsg := sut.deployEscrow(t, big.NewInt(escrowDepositVal))

	vmTime.advanceToSettle(ctx, t, b)
	for range 2 {
		bb := sut.runConsensusLoop(t)
		vmTime.advanceToSettle(ctx, t, bb)
	}
	_, ok := sut.rawVM.consensusCritical.Load(b.Hash())
	require.Falsef(t, ok, "%T[%#x] still in VM memory", b, b.Hash())

	// Storage key for balances[recv] at mapping slot 0:
	// keccak256(abi.encode(address, uint256(0)))
	storageKey := crypto.Keccak256Hash(
		common.LeftPadBytes(recv.Bytes(), 32),
		common.Hash{}.Bytes(),
	)
	storageKeyHex := storageKey.Hex()

	gc := gethclient.New(sut.rpcClient)

	wantBalance := big.NewInt(escrowDepositVal)
	wantStorageValue := big.NewInt(escrowDepositVal)
	wantStorageBytes := uint256.NewInt(escrowDepositVal).PaddedBytes(32)
	wantCode := escrow.ByteCode()

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
				require.Zero(t, wantBalance.Cmp(got), "BalanceAt(): want %d, got %s", wantBalance, got)
			})

			t.Run("eth_getCode", func(t *testing.T) {
				got, err := sut.CodeAt(ctx, escrowAddr, blockNum)
				require.NoError(t, err, "CodeAt()")
				assert.Equal(t, wantCode, got, "CodeAt() result")
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

				assert.NotEmpty(t, got.AccountProof, "GetProof() accountProof")
				require.Zero(t, wantBalance.Cmp(got.Balance), "GetProof() balance: want %d, got %s", wantBalance, got.Balance)

				require.Len(t, got.StorageProof, 1, "GetProof() storageProof length")
				assert.NotEmpty(t, got.StorageProof[0].Proof, "GetProof() storageProof[0].Proof")
				require.Zero(t, wantStorageValue.Cmp(got.StorageProof[0].Value), "GetProof() storageProof[0].Value: want %d, got %s", wantStorageValue, got.StorageProof[0].Value)
			})
		})
	}
}

// TestStatefulRPCsLatestOnly tests stateful RPC methods that don't accept a
// block number parameter via ethclient/gethclient and so always run against
// the latest block: eth_estimateGas and eth_createAccessList.
func TestStatefulRPCsLatestOnly(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	_, _, _, _, callMsg := sut.deployEscrow(t, nil)

	t.Run("eth_estimateGas", func(t *testing.T) {
		got, err := sut.EstimateGas(ctx, callMsg)
		require.NoError(t, err, "EstimateGas()")
		assert.Positive(t, got, "EstimateGas() result")
	})

	t.Run("eth_createAccessList", func(t *testing.T) {
		gc := gethclient.New(sut.rpcClient)
		al, gas, errMsg, err := gc.CreateAccessList(ctx, callMsg)
		require.NoError(t, err, "CreateAccessList()")
		assert.Empty(t, errMsg, "CreateAccessList() error message")
		assert.NotEmpty(t, al, "CreateAccessList() access list")
		assert.Positive(t, gas, "CreateAccessList() gasUsed")
	})
}
