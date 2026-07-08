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
	"github.com/ava-labs/libevm/core"
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

// txTraceResult is the per-transaction result returned by geth's
// debug_traceBlock* RPCs.
type txTraceResult struct {
	TxHash common.Hash             `json:"txHash"`
	Result *logger.ExecutionResult `json:"result"`
	Error  string                  `json:"error"`
}

// depositLogPC is the program counter, in [escrow.ByteCode], of the LOG1
// opcode for `emit Deposit()`. Verified by [depositLogOnlyCmpOpts].
const depositLogPC = 185

// depositLogOnlyCmpOpts returns cmp options that limit a trace comparison to
// the [depositLogPC] entry, ignoring its gas fields; asserting a full trace
// would just be copy-pasted test output.
func depositLogOnlyCmpOpts(tb testing.TB) cmp.Options {
	tb.Helper()
	require.Equal(tb, vm.LOG1, vm.OpCode(escrow.ByteCode()[depositLogPC]), "Bad test setup; program counter LOG1 for `emit Deposit()`")
	return cmp.Options{
		cmpopts.IgnoreSliceElements(func(r logger.StructLogRes) bool {
			return r.Pc != depositLogPC || r.Op != vm.LOG1.String()
		}),
		cmpopts.IgnoreFields(logger.StructLogRes{}, "Gas", "GasCost"),
	}
}

// deployTraceResult returns the expected trace, under
// [depositLogOnlyCmpOpts], of an [escrow.CreationCode] deployment.
func deployTraceResult(tx *types.Transaction, gasUsed uint64) []txTraceResult {
	return []txTraceResult{{
		TxHash: tx.Hash(),
		Result: &logger.ExecutionResult{
			Gas:         gasUsed,
			ReturnValue: common.Bytes2Hex(escrow.ByteCode()),
			StructLogs:  []logger.StructLogRes{},
		},
	}}
}

// depositTraceResult returns the expected trace, under
// [depositLogOnlyCmpOpts], of an [escrow.CallDataToDeposit] call.
func depositTraceResult(tx *types.Transaction, gasUsed uint64, recipient common.Address, depositVal uint64) []txTraceResult {
	return []txTraceResult{{
		TxHash: tx.Hash(),
		Result: &logger.ExecutionResult{
			Gas: gasUsed,
			StructLogs: []logger.StructLogRes{{
				Pc:    depositLogPC,
				Op:    vm.LOG1.String(),
				Depth: 1,
				Stack: utils.PointerTo([]string{
					escrow.DepositEvent(recipient, uint256.NewInt(depositVal)).Topics[0].String(),
					"0x40", "0x80", // arbitrary memory locations selected by Solidity
				}),
			}},
		},
	}}
}

// TestRPCsWithSynchronousHistory verifies state, receipt, and tracing RPCs on
// a chain seeded with synchronous (pre-SAE) history, as inherited from a
// migrated chain (e.g. the C-Chain), plus an asynchronous block built on the
// synchronous frontier.
func TestRPCsWithSynchronousHistory(t *testing.T) {
	const (
		numSyncBlocks    = 2
		escrowDepositVal = 42
	)
	recipient := common.Address{'r', 'e', 'c', 'v'}
	var (
		escrowAddr   common.Address
		syncChain    []*types.Block
		syncReceipts []types.Receipts
	)
	seedOpt := withSynchronousChain(numSyncBlocks, func(w *saetest.Wallet, i int, bg *core.BlockGen) {
		switch i {
		case 0:
			escrowAddr = crypto.CreateAddress(w.Addresses()[0], 0)
			bg.AddTx(w.SetNonceAndSign(t, 0, &types.LegacyTx{
				Gas:      1e6,
				GasPrice: big.NewInt(1),
				Data:     escrow.CreationCode(),
			}))
		case 1:
			bg.AddTx(w.SetNonceAndSign(t, 0, &types.LegacyTx{
				To:       &escrowAddr,
				Gas:      1e6,
				GasPrice: big.NewInt(1),
				Data:     escrow.CallDataToDeposit(recipient),
				Value:    big.NewInt(escrowDepositVal),
			}))
		}
	}, &syncChain, &syncReceipts)

	// The VM's clock MUST NOT be before the seeded chain's tip, whose
	// timestamp is dictated by [core.GenerateChain]'s 10-second block gap.
	timeOpt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds+10*numSyncBlocks, 0))
	ctx, sut := newSUT(t, 1, seedOpt, timeOpt)

	deploy, deposit := syncChain[0], syncChain[1]
	frontier := sut.lastAcceptedBlock(t)
	require.Equalf(t, deposit.Hash(), frontier.Hash(), "%T.lastAcceptedBlock() after initialization on seeded chain", sut)
	require.Truef(t, frontier.Synchronous(), "%T.Synchronous()", frontier)

	deployTx := deploy.Transactions()[0]
	depositTx := deposit.Transactions()[0]

	ignore := depositLogOnlyCmpOpts(t)
	wantDeployTrace := deployTraceResult(deployTx, syncReceipts[0][0].GasUsed)
	wantDepositTrace := depositTraceResult(depositTx, syncReceipts[1][0].GasUsed, recipient, escrowDepositVal)

	sender := sut.wallet.Addresses()[0]
	balanceCall := map[string]any{
		"to":   escrowAddr,
		"data": hexutil.Bytes(escrow.CallDataForBalance(recipient)),
	}
	genesisHash := deploy.ParentHash()
	genesisBalance := (*hexutil.Big)(new(uint256.Int).SetAllOne().ToBig()) // [saetest.MaxAllocFor]
	tests := []rpcTest{
		// Synchronous genesis (no receipts).
		{
			method: "eth_getBalance",
			args:   []any{sender, rpc.BlockNumberOrHashWithNumber(0)},
			want:   genesisBalance,
		},
		{
			method: "eth_getBalance",
			args:   []any{sender, rpc.BlockNumberOrHashWithHash(genesisHash, true)},
			want:   genesisBalance,
		},
		{
			method: "eth_getBlockReceipts",
			args:   []any{hexutil.Uint64(0)},
			want:   []*types.Receipt{},
		},
		{
			method: "eth_getBlockReceipts",
			args:   []any{genesisHash},
			want:   []*types.Receipt{},
		},
		// Synchronous blocks.
		{
			method: "eth_getBalance",
			args:   []any{escrowAddr, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(deposit.Number().Int64()))},
			want:   hexBig(escrowDepositVal),
		},
		{
			method: "eth_getBalance",
			args:   []any{escrowAddr, rpc.BlockNumberOrHashWithHash(deposit.Hash(), true)},
			want:   hexBig(escrowDepositVal),
		},
		{
			method: "eth_call",
			args:   []any{balanceCall, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(deploy.Number().Int64()))},
			want:   hexutil.Bytes(make([]byte, 32)),
		},
		{
			method: "eth_call",
			args:   []any{balanceCall, rpc.BlockNumberOrHashWithHash(deposit.Hash(), true)},
			want:   hexutil.Bytes(uint256.NewInt(escrowDepositVal).PaddedBytes(32)),
		},
		{
			method: "eth_getTransactionCount",
			args:   []any{sender, rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(deploy.Number().Int64()))},
			want:   hexutil.Uint64(1),
		},
		{
			method: "eth_getTransactionCount",
			args:   []any{sender, rpc.BlockNumberOrHashWithHash(deposit.Hash(), true)},
			want:   hexutil.Uint64(2),
		},
		{
			method:       "eth_getBlockReceipts",
			args:         []any{deploy.Hash()},
			want:         []*types.Receipt(syncReceipts[0]),
			extraCmpOpts: cmp.Options{cmputils.NilSlicesAreEmpty[[]*types.Log]()},
		},
		{
			method: "eth_getBlockReceipts",
			args:   []any{hexutil.Uint64(deposit.NumberU64())},
			want:   []*types.Receipt(syncReceipts[1]),
		},
		{
			method: "eth_getTransactionReceipt",
			args:   []any{depositTx.Hash()},
			want:   syncReceipts[1][0],
		},
		// Traces re-execute sync blocks.
		{
			method:       "debug_traceBlockByNumber",
			args:         []any{hexutil.Uint64(deploy.NumberU64())},
			want:         wantDeployTrace,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceBlockByNumber",
			args:         []any{hexutil.Uint64(deposit.NumberU64())},
			want:         wantDepositTrace,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceBlockByHash",
			args:         []any{deposit.Hash()},
			want:         wantDepositTrace,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceTransaction",
			args:         []any{depositTx.Hash()},
			want:         *wantDepositTrace[0].Result,
			extraCmpOpts: ignore,
		},
		// Re-executing a sync block reproduces its header state root.
		{
			method: "debug_intermediateRoots",
			args:   []any{deploy.Hash()},
			want:   []common.Hash{deploy.Root()},
		},
		{
			method: "debug_intermediateRoots",
			args:   []any{deposit.Hash()},
			want:   []common.Hash{deposit.Root()},
		},
	}

	t.Run("synchronous_frontier_in_memory", func(t *testing.T) {
		sut.testRPC(ctx, t, tests...)
	})

	// Asynchronous blocks MUST build on the synchronous frontier.
	deposit2Tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &escrowAddr,
		Gas:      1e6,
		GasPrice: big.NewInt(1),
		Data:     escrow.CallDataToDeposit(recipient),
		Value:    big.NewInt(escrowDepositVal),
	})
	async := sut.runConsensusLoop(t, deposit2Tx)
	asyncReceipts := async.Receipts() // blocks until executed
	require.Lenf(t, asyncReceipts, 1, "%T.Receipts()", async)

	sut.settleUntilEvicted(t, vmTime, deposit.Hash(), async.Hash())

	// The restored async block, built on a sync parent.
	wantAsyncTrace := depositTraceResult(deposit2Tx, asyncReceipts[0].GasUsed, recipient, escrowDepositVal)
	tests = append(tests, []rpcTest{
		{
			method: "eth_getBlockReceipts",
			args:   []any{async.Hash()},
			want:   []*types.Receipt(asyncReceipts),
		},
		{
			method: "eth_getTransactionReceipt",
			args:   []any{deposit2Tx.Hash()},
			want:   asyncReceipts[0],
		},
		{
			method:       "debug_traceBlockByNumber",
			args:         []any{hexutil.Uint64(async.NumberU64())},
			want:         wantAsyncTrace,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceTransaction",
			args:         []any{deposit2Tx.Hash()},
			want:         *wantAsyncTrace[0].Result,
			extraCmpOpts: ignore,
		},
	}...)

	t.Run("synchronous_frontier_on_disk", func(t *testing.T) {
		sut.testRPC(ctx, t, tests...)
	})
}

func TestDebugTrace(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	deployBlock, escrowAddr, deployTx := sut.deployEscrow(t)

	const escrowDepositVal = 42
	recipient := common.Address{'r', 'e', 'c', 'v'}
	depositBlock, depositTx := sut.depositToEscrow(t, escrowAddr, recipient, big.NewInt(escrowDepositVal))

	ignore := depositLogOnlyCmpOpts(t)
	wantDeploy := deployTraceResult(deployTx, deployBlock.Receipts()[0].GasUsed)
	wantDeposit := depositTraceResult(depositTx, depositBlock.Receipts()[0].GasUsed, recipient, escrowDepositVal)

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
			method:       "debug_traceTransaction",
			args:         []any{deployTx.Hash()},
			want:         *wantDeploy[0].Result,
			extraCmpOpts: ignore,
		},
		{
			method:       "debug_traceTransaction",
			args:         []any{depositTx.Hash()},
			want:         *wantDeposit[0].Result,
			extraCmpOpts: ignore,
		},
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

	sut.settleUntilEvicted(t, vmTime, b.Hash())

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
