// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracers

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
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

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/internal/ethapi"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/subnet-evm/precompile/contracts/txallowlist"
	"github.com/ava-labs/subnet-evm/rpc"

	ethparams "github.com/ava-labs/libevm/params"
)

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
		result, err := api.TraceBlockByNumber(context.Background(), tc.blockNumber, tc.config)
		if tc.expectErr != nil {
			if err == nil {
				t.Errorf("test %d, want error %v", i, tc.expectErr)
				continue
			}
			if !reflect.DeepEqual(err, tc.expectErr) {
				t.Errorf("test %d: error mismatch, want %v, get %v", i, tc.expectErr, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("test %d, want no error, have %v", i, err)
			continue
		}
		have, _ := json.Marshal(result)
		want := tc.want
		if string(have) != want {
			t.Errorf("test %d, result mismatch, have\n%v\n, want\n%v\n", i, string(have), want)
		}
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
			result, err := api.TraceTransaction(context.Background(), target, nil)
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

		from, _ := api.blockByNumber(context.Background(), rpc.BlockNumber(c.start))
		to, _ := api.blockByNumber(context.Background(), rpc.BlockNumber(c.end))
		resCh := api.traceChain(from, to, c.config, nil)

		next := c.start + 1
		for result := range resCh {
			if have, want := uint64(result.Block), next; have != want {
				t.Fatalf("unexpected tracing block, have %d want %d", have, want)
			}
			if have, want := len(result.Traces), int(next); have != want {
				t.Fatalf("unexpected result length, have %d want %d", have, want)
			}
			for _, trace := range result.Traces {
				trace.TxHash = common.Hash{}
				blob, _ := json.Marshal(trace)
				if have, want := string(blob), single; have != want {
					t.Fatalf("unexpected tracing result, have\n%v\nwant:\n%v", have, want)
				}
			}
			next += 1
		}
		if next != c.end+1 {
			t.Error("Missing tracing block")
		}

		if nref, nrel := ref.Load(), rel.Load(); nref != nrel {
			t.Errorf("Ref and deref actions are not equal, ref %d rel %d", nref, nrel)
		}
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
			blockNumber: rpc.BlockNumber(activateStateUpgradeBlock - 1),
			call: ethapi.TransactionArgs{
				From:  &accounts[2].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			config: &TraceCallConfig{
				BlockOverrides: &ethapi.BlockOverrides{
					Time: (*hexutil.Uint64)(&fastForwardTime),
				},
			},
			expectErr: core.ErrInsufficientFunds,
			expect:    `{"gas":21000,"failed":true,"returnValue":"","structLogs":[]}`,
		},
	}
	for i, testspec := range testSuite {
		result, err := api.TraceCall(context.Background(), testspec.call, rpc.BlockNumberOrHash{BlockNumber: &testspec.blockNumber}, testspec.config)
		if testspec.expectErr != nil {
			require.ErrorIs(t, err, testspec.expectErr, "test %d", i)
			continue
		} else {
			if err != nil {
				t.Errorf("test %d: expect no error, got %v", i, err)
				continue
			}
			var have *logger.ExecutionResult
			if err := json.Unmarshal(result.(json.RawMessage), &have); err != nil {
				t.Errorf("test %d: failed to unmarshal result %v", i, err)
			}
			var want *logger.ExecutionResult
			if err := json.Unmarshal([]byte(testspec.expect), &want); err != nil {
				t.Errorf("test %d: failed to unmarshal result %v", i, err)
			}
			if !reflect.DeepEqual(have, want) {
				t.Errorf("test %d: result mismatch, want %v, got %v", i, testspec.expect, string(result.(json.RawMessage)))
			}
		}
	}
}
