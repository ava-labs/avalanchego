// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracers

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/eth/tracers/logger"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/internal/ethapi"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/rpc"

	ethparams "github.com/ava-labs/libevm/params"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

var schemes = []string{rawdb.HashScheme, customrawdb.FirewoodScheme}

func TestTraceBlockPrecompileActivation(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testTraceBlockPrecompileActivation(t, scheme)
		})
	}
}

func testTraceBlockPrecompileActivation(t *testing.T, scheme string) {
	t.Parallel()

	// Initialize test accounts
	accounts := newAccounts(3)
	copyConfig := params.Copy(params.TestChainConfig)
	genesis := &core.Genesis{
		Config: &copyConfig,
		Alloc: types.GenesisAlloc{
			accounts[0].addr: {Balance: big.NewInt(params.Ether)},
			accounts[1].addr: {Balance: big.NewInt(params.Ether)},
			accounts[2].addr: {Balance: big.NewInt(params.Ether)},
		},
	}
	activateAllowlistBlock := 3
	// assumes gap is 10 sec
	activateAllowListTime := uint64(activateAllowlistBlock * 10)
	activateTxAllowListConfig := extras.PrecompileUpgrade{
		Config: txallowlist.NewConfig(&activateAllowListTime, []common.Address{accounts[0].addr}, nil, nil),
	}

	deactivateAllowlistBlock := activateAllowlistBlock + 3
	deactivateAllowListTime := uint64(deactivateAllowlistBlock) * 10
	deactivateTxAllowListConfig := extras.PrecompileUpgrade{
		Config: txallowlist.NewDisableConfig(&deactivateAllowListTime),
	}

	params.GetExtra(genesis.Config).PrecompileUpgrades = []extras.PrecompileUpgrade{
		activateTxAllowListConfig,
		deactivateTxAllowListConfig,
	}
	genBlocks := 10
	signer := types.HomesteadSigner{}
	txHashes := make([]common.Hash, genBlocks)
	backend := newTestBackend(t, genBlocks, genesis, scheme, func(i int, b *core.BlockGen) {
		// Transfer from account[0] to account[1]
		//    value: 1000 wei
		//    fee:   0 wei
		tx, _ := types.SignTx(types.NewTransaction(uint64(i), accounts[1].addr, big.NewInt(1000), ethparams.TxGas, b.BaseFee(), nil), signer, accounts[0].key)
		b.AddTx(tx)
		txHashes[i] = tx.Hash()
	})
	defer backend.chain.Stop()
	api := NewAPI(backend)

	testSuite := []struct {
		blockNumber rpc.BlockNumber
		config      *TraceConfig
		want        string
		expectErr   error
	}{
		// Trace head block
		{
			blockNumber: rpc.BlockNumber(genBlocks),
			want:        fmt.Sprintf(`[{"txHash":"%v","result":{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}}]`, txHashes[genBlocks-1]),
		},
		// Trace block before activation
		{
			blockNumber: rpc.BlockNumber(activateAllowlistBlock - 1),
			want:        fmt.Sprintf(`[{"txHash":"%v","result":{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}}]`, txHashes[activateAllowlistBlock-2]),
		},
		// Trace block activation
		{
			blockNumber: rpc.BlockNumber(activateAllowlistBlock),
			want:        fmt.Sprintf(`[{"txHash":"%v","result":{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}}]`, txHashes[activateAllowlistBlock-1]),
		},
		// Trace block after activation
		{
			blockNumber: rpc.BlockNumber(activateAllowlistBlock + 1),
			want:        fmt.Sprintf(`[{"txHash":"%v","result":{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}}]`, txHashes[activateAllowlistBlock]),
		},
		// Trace block deactivation
		{
			blockNumber: rpc.BlockNumber(deactivateAllowlistBlock),
			want:        fmt.Sprintf(`[{"txHash":"%v","result":{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}}]`, txHashes[deactivateAllowlistBlock-1]),
		},
		// Trace block after deactivation
		{
			blockNumber: rpc.BlockNumber(deactivateAllowlistBlock + 1),
			want:        fmt.Sprintf(`[{"txHash":"%v","result":{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}}]`, txHashes[deactivateAllowlistBlock]),
		},
	}
	for i, tc := range testSuite {
		result, err := api.TraceBlockByNumber(t.Context(), tc.blockNumber, tc.config)
		require.ErrorIs(t, err, tc.expectErr, "test %d", i)
		if tc.expectErr != nil {
			continue
		}
		have, _ := json.Marshal(result)
		require.Equal(t, tc.want, string(have), "test %d", i)
	}
}

func TestTraceTransactionPrecompileActivation(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testTraceTransactionPrecompileActivation(t, scheme)
		})
	}
}

func testTraceTransactionPrecompileActivation(t *testing.T, scheme string) {
	t.Parallel()

	// Initialize test accounts
	accounts := newAccounts(3)
	copyConfig := params.Copy(params.TestChainConfig)
	genesis := &core.Genesis{
		Config: &copyConfig,
		Alloc: types.GenesisAlloc{
			accounts[0].addr: {Balance: big.NewInt(params.Ether)},
			accounts[1].addr: {Balance: big.NewInt(params.Ether)},
			accounts[2].addr: {Balance: big.NewInt(params.Ether)},
		},
	}
	activateAllowlistBlock := uint64(2)
	// assumes gap is 10 sec
	activateAllowListTime := activateAllowlistBlock * 10
	activateTxAllowListConfig := extras.PrecompileUpgrade{
		Config: txallowlist.NewConfig(&activateAllowListTime, []common.Address{accounts[0].addr}, nil, nil),
	}

	deactivateAllowlistBlock := activateAllowlistBlock + 2
	deactivateAllowListTime := deactivateAllowlistBlock * 10
	deactivateTxAllowListConfig := extras.PrecompileUpgrade{
		Config: txallowlist.NewDisableConfig(&deactivateAllowListTime),
	}

	params.GetExtra(genesis.Config).PrecompileUpgrades = []extras.PrecompileUpgrade{
		activateTxAllowListConfig,
		deactivateTxAllowListConfig,
	}
	signer := types.HomesteadSigner{}
	blockNoTxMap := make(map[uint64]common.Hash)

	backend := newTestBackend(t, 5, genesis, scheme, func(i int, b *core.BlockGen) {
		// Transfer from account[0] to account[1]
		//    value: 1000 wei
		//    fee:   0 wei
		tx, _ := types.SignTx(types.NewTransaction(uint64(i), accounts[1].addr, big.NewInt(1000), ethparams.TxGas, new(big.Int).Add(b.BaseFee(), big.NewInt(int64(500*params.GWei))), nil), signer, accounts[0].key)
		b.AddTx(tx)
		blockNoTxMap[uint64(i)] = tx.Hash()
	})

	defer backend.chain.Stop()
	api := NewAPI(backend)
	for i, target := range blockNoTxMap {
		t.Run(fmt.Sprintf("blockNumber %d", i), func(t *testing.T) {
			result, err := api.TraceTransaction(t.Context(), target, nil)
			require := require.New(t)
			require.NoError(err)
			var have *logger.ExecutionResult
			require.NoError(json.Unmarshal(result.(json.RawMessage), &have))
			expected := &logger.ExecutionResult{
				Gas:         ethparams.TxGas,
				Failed:      false,
				ReturnValue: "",
				StructLogs:  []logger.StructLogRes{},
			}
			eq := reflect.DeepEqual(have, expected)
			require.True(eq, "have %v, want %v", have, expected)
		})
	}
}

func TestTraceChainPrecompileActivation(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testTraceChainPrecompileActivation(t, scheme)
		})
	}
}

func testTraceChainPrecompileActivation(t *testing.T, scheme string) {
	// Initialize test accounts
	// Note: the balances in this test have been increased compared to go-ethereum.
	accounts := newAccounts(3)
	copyConfig := params.Copy(params.TestChainConfig)
	genesis := &core.Genesis{
		Config: &copyConfig,
		Alloc: types.GenesisAlloc{
			accounts[0].addr: {Balance: big.NewInt(5 * params.Ether)},
			accounts[1].addr: {Balance: big.NewInt(5 * params.Ether)},
			accounts[2].addr: {Balance: big.NewInt(5 * params.Ether)},
		},
	}
	activateAllowlistBlock := uint64(20)
	// assumes gap is 10 sec
	activateAllowListTime := activateAllowlistBlock * 10
	activateTxAllowListConfig := extras.PrecompileUpgrade{
		Config: txallowlist.NewConfig(&activateAllowListTime, []common.Address{accounts[0].addr}, nil, nil),
	}

	deactivateAllowlistBlock := activateAllowlistBlock + 10
	deactivateAllowListTime := deactivateAllowlistBlock * 10
	deactivateTxAllowListConfig := extras.PrecompileUpgrade{
		Config: txallowlist.NewDisableConfig(&deactivateAllowListTime),
	}

	params.GetExtra(genesis.Config).PrecompileUpgrades = []extras.PrecompileUpgrade{
		activateTxAllowListConfig,
		deactivateTxAllowListConfig,
	}
	genBlocks := 50
	signer := types.HomesteadSigner{}

	var (
		ref   atomic.Uint32 // total refs has made
		rel   atomic.Uint32 // total rels has made
		nonce uint64
	)
	backend := newTestBackend(t, genBlocks, genesis, scheme, func(i int, b *core.BlockGen) {
		// Transfer from account[0] to account[1]
		//    value: 1000 wei
		//    fee:   0 wei
		for j := 0; j < i+1; j++ {
			tx, _ := types.SignTx(types.NewTransaction(nonce, accounts[1].addr, big.NewInt(1000), ethparams.TxGas, b.BaseFee(), nil), signer, accounts[0].key)
			b.AddTx(tx)
			nonce += 1
		}
	})
	backend.refHook = func() { ref.Add(1) }
	backend.relHook = func() { rel.Add(1) }
	api := NewAPI(backend)

	single := `{"txHash":"0x0000000000000000000000000000000000000000000000000000000000000000","result":{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}}`
	cases := []struct {
		start  uint64
		end    uint64
		config *TraceConfig
	}{
		{0, 50, nil},  // the entire chain range, blocks [1, 50]
		{10, 20, nil}, // the middle chain range, blocks [11, 20]
	}
	for _, c := range cases {
		ref.Store(0)
		rel.Store(0)

		from, _ := api.blockByNumber(t.Context(), rpc.BlockNumber(c.start))
		to, _ := api.blockByNumber(t.Context(), rpc.BlockNumber(c.end))
		resCh := api.traceChain(from, to, c.config, nil)

		next := c.start + 1
		for result := range resCh {
			require.Equal(t, next, uint64(result.Block), "unexpected tracing block")
			require.Len(t, result.Traces, int(next), "unexpected result length")
			for _, trace := range result.Traces {
				trace.TxHash = common.Hash{}
				blob, _ := json.Marshal(trace)
				require.Equal(t, single, string(blob), "unexpected tracing result")
			}
			next += 1
		}
		require.Equal(t, c.end+1, next, "Missing tracing block")

		nref, nrel := ref.Load(), rel.Load()
		require.Equal(t, nrel, nref, "Ref and deref actions are not equal")
	}
}

func TestTraceCallWithOverridesStateUpgrade(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testTraceCallWithOverridesStateUpgrade(t, scheme)
		})
	}
}

func testTraceCallWithOverridesStateUpgrade(t *testing.T, scheme string) {
	t.Parallel()

	// Initialize test accounts
	accounts := newAccounts(3)
	copyConfig := params.Copy(params.TestChainConfig)
	genesis := &core.Genesis{
		Config: &copyConfig,
		Alloc: types.GenesisAlloc{
			accounts[0].addr: {Balance: big.NewInt(5 * params.Ether)},
			accounts[1].addr: {Balance: big.NewInt(5 * params.Ether)},
			accounts[2].addr: {Balance: big.NewInt(5 * params.Ether)},
		},
	}
	activateStateUpgradeBlock := uint64(2)
	// assumes gap is 10 sec
	activateStateUpgradeTime := activateStateUpgradeBlock * 10
	activateStateUpgradeConfig := extras.StateUpgrade{
		BlockTimestamp: &activateStateUpgradeTime,
		StateUpgradeAccounts: map[common.Address]extras.StateUpgradeAccount{
			accounts[2].addr: {
				// deplete all balance
				BalanceChange: (*math.HexOrDecimal256)(new(big.Int).Neg(big.NewInt(5 * params.Ether))),
			},
		},
	}

	params.GetExtra(genesis.Config).StateUpgrades = []extras.StateUpgrade{
		activateStateUpgradeConfig,
	}
	genBlocks := 3
	// assumes gap is 10 sec
	signer := types.HomesteadSigner{}
	fastForwardTime := activateStateUpgradeTime + 10
	backend := newTestBackend(t, genBlocks, genesis, scheme, func(i int, b *core.BlockGen) {
		// Transfer from account[0] to account[1]
		//    value: 1000 wei
		//    fee:   0 wei
		tx, _ := types.SignTx(types.NewTransaction(uint64(i), accounts[1].addr, big.NewInt(1000), ethparams.TxGas, b.BaseFee(), nil), signer, accounts[0].key)
		b.AddTx(tx)
	})
	defer backend.teardown()
	api := NewAPI(backend)
	testSuite := []struct {
		blockNumber rpc.BlockNumber
		call        ethapi.TransactionArgs
		config      *TraceCallConfig
		expectErr   error
		expect      string
	}{
		{
			blockNumber: rpc.BlockNumber(0),
			call: ethapi.TransactionArgs{
				From:  &accounts[0].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			config:    nil,
			expectErr: nil,
			expect:    `{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}`,
		},
		{
			blockNumber: rpc.BlockNumber(activateStateUpgradeBlock - 1),
			call: ethapi.TransactionArgs{
				From:  &accounts[2].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			config:    nil,
			expectErr: nil,
			expect:    `{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}`,
		},
		{
			blockNumber: rpc.BlockNumber(activateStateUpgradeBlock + 1),
			call: ethapi.TransactionArgs{
				From:  &accounts[2].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			config:    nil,
			expectErr: core.ErrInsufficientFunds,
			expect:    `{"gas":21000,"failed":true,"returnValue":"","structLogs":[]}`,
		},
		{
			// Test that state upgrades are NOT applied when only time is overridden.
			// Even though we override time to after the state upgrade, the upgrade
			// should not be applied because it uses the original block time.
			blockNumber: rpc.BlockNumber(activateStateUpgradeBlock - 1),
			call: ethapi.TransactionArgs{
				From:  &accounts[2].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			config: &TraceCallConfig{
				BlockOverrides: &ethapi.BlockOverrides{
					Number: (*hexutil.Big)(big.NewInt(int64(activateStateUpgradeBlock - 1))),
					Time:   (*hexutil.Uint64)(&fastForwardTime),
				},
			},
			expectErr: nil,
			expect:    `{"gas":21000,"failed":false,"returnValue":"","structLogs":[]}`,
		},
	}
	for i, testspec := range testSuite {
		result, err := api.TraceCall(t.Context(), testspec.call, rpc.BlockNumberOrHash{BlockNumber: &testspec.blockNumber}, testspec.config)
		require.ErrorIs(t, err, testspec.expectErr, "test %d", i)
		if err != nil {
			continue
		}
		var have *logger.ExecutionResult
		require.NoError(t, json.Unmarshal(result.(json.RawMessage), &have), "test %d: failed to unmarshal result", i)
		var want *logger.ExecutionResult
		require.NoError(t, json.Unmarshal([]byte(testspec.expect), &want), "test %d: failed to unmarshal result", i)
		require.Equal(t, want, have, "test %d: result mismatch", i)
	}
}
