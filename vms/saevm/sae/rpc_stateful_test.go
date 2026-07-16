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
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/rpc"
	"github.com/ava-labs/libevm/trie"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
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
	ctx, sut := newSUT(t, 1)

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

	// callFrame is the JSON encoding of the native callTracer's output.
	// TODO(JonathanOppenheimer): export from libevm?
	type callFrame struct {
		From    common.Address  `json:"from"`
		Gas     hexutil.Uint64  `json:"gas"`
		GasUsed hexutil.Uint64  `json:"gasUsed"`
		To      *common.Address `json:"to"`
		Input   hexutil.Bytes   `json:"input"`
		Value   *hexutil.Big    `json:"value"`
		Type    string          `json:"type"`
		Error   string          `json:"error"`
	}

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
		{
			method: "debug_traceTransaction",
			args: []any{depositTx.Hash(), map[string]any{
				"tracer":  `{fault: function() {}, result: function() { for (;;) {} }}`,
				"timeout": "10ms",
			}},
			wantErr: testerr.Contains("execution timeout"),
		},
		{
			method: "debug_traceTransaction",
			args:   []any{depositTx.Hash(), map[string]any{"tracer": "callTracer"}},
			want: callFrame{
				From:    sut.wallet.Addresses()[0],
				Gas:     hexutil.Uint64(depositTx.Gas()),
				GasUsed: hexutil.Uint64(depositBlock.Receipts()[0].GasUsed),
				To:      &escrowAddr,
				Input:   escrow.CallDataToDeposit(recipient),
				Value:   (*hexutil.Big)(big.NewInt(escrowDepositVal)),
				Type:    "CALL",
			},
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

				verifyProof(t, b.PostExecutionStateRoot(), got)
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

func verifyProof(tb testing.TB, root common.Hash, proof *gethclient.AccountResult) {
	tb.Helper()

	account := proveAccount(tb, root, proof.Address, proof.AccountProof)
	assert.Zerof(tb, account.Balance.ToBig().Cmp(proof.Balance), "proven account balance: proven %d, claimed %s", account.Balance.ToBig(), proof.Balance)
	assert.Equal(tb, proof.CodeHash[:], account.CodeHash, "proven account code hash")
	assert.Equal(tb, proof.Nonce, account.Nonce, "proven account nonce")
	assert.Equal(tb, proof.StorageHash, account.Root, "proven account storage root")

	for _, sp := range proof.StorageProof {
		value := proveStorageValue(tb, account.Root, common.HexToHash(sp.Key), sp.Proof)
		assert.Zerof(tb, sp.Value.Cmp(value), "proven storage value: proven %d, claimed %s", value, sp.Value)
	}
}

func proveAccount(tb testing.TB, root common.Hash, addr common.Address, nodes []string) types.StateAccount {
	tb.Helper()

	accountRLP := proveTrieValue(tb, root, crypto.Keccak256(addr.Bytes()), nodes)
	var account types.StateAccount
	require.NoError(tb, rlp.DecodeBytes(accountRLP, &account), "decode proven account")
	return account
}

func proveStorageValue(tb testing.TB, root common.Hash, key common.Hash, nodes []string) *big.Int {
	tb.Helper()

	storageRLP := proveTrieValue(tb, root, crypto.Keccak256(key.Bytes()), nodes)
	var value big.Int
	require.NoError(tb, rlp.DecodeBytes(storageRLP, &value), "decode proven storage value")
	return &value
}

func proveTrieValue(tb testing.TB, root common.Hash, key []byte, nodes []string) []byte {
	tb.Helper()

	proofDB := memorydb.New()
	for _, nodeStr := range nodes {
		nodeBytes := common.FromHex(nodeStr)
		nodeHash := crypto.Keccak256(nodeBytes)
		require.NoErrorf(tb, proofDB.Put(nodeHash, nodeBytes), "%T.Put(proof node)", proofDB)
	}

	value, err := trie.VerifyProof(root, key, proofDB)
	require.NoError(tb, err, "VerifyProof()")
	return value
}
