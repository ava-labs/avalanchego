// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package gasprice

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/avalanchego/graft/evm/rpc"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/event"
	ethparams "github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

var (
	key, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	addr   = crypto.PubkeyToAddress(key.PublicKey)
	bal, _ = new(big.Int).SetString("100000000000000000000000", 10)
)

type testBackend struct {
	chain         *core.BlockChain
	acceptedEvent chan<- core.ChainEvent
}

func (b *testBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	if number == rpc.LatestBlockNumber {
		return b.chain.CurrentBlock(), nil
	}
	return b.chain.GetHeaderByNumber(uint64(number)), nil
}

func (b *testBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	if number == rpc.LatestBlockNumber {
		number = rpc.BlockNumber(b.chain.CurrentBlock().Number.Uint64())
	}
	return b.chain.GetBlockByNumber(uint64(number)), nil
}

func (b *testBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return b.chain.GetReceiptsByHash(hash), nil
}

func (b *testBackend) ChainConfig() *params.ChainConfig {
	return b.chain.Config()
}

func (b *testBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return nil
}

func (b *testBackend) SubscribeChainAcceptedEvent(ch chan<- core.ChainEvent) event.Subscription {
	b.acceptedEvent = ch
	return nil
}

func (b *testBackend) teardown() {
	b.chain.Stop()
}

func newTestBackendFakerEngine(t *testing.T, numBlocks int, genBlocks func(i int, b *core.BlockGen)) *testBackend {
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{addr: {Balance: bal}},
	}

	engine := dummy.NewETHFaker()

	// Generate testing blocks
	_, blocks, _, err := core.GenerateChainWithGenesis(gspec, engine, numBlocks, ap4.TargetBlockRate-1, genBlocks)
	if err != nil {
		t.Fatal(err)
	}
	// Construct testing chain
	diskdb := rawdb.NewMemoryDatabase()
	chain, err := core.NewBlockChain(diskdb, core.DefaultCacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
	if err != nil {
		t.Fatalf("Failed to create local chain, %v", err)
	}
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("Failed to insert chain, %v", err)
	}
	return &testBackend{chain: chain}
}

// newTestBackend creates a test backend. OBS: don't forget to invoke tearDown
// after use, otherwise the blockchain instance will mem-leak via goroutines.
func newTestBackend(t *testing.T, numBlocks int, genBlocks func(i int, b *core.BlockGen)) *testBackend {
	gspec := &core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  types.GenesisAlloc{addr: {Balance: bal}},
	}

	engine := dummy.NewFaker()

	// Generate testing blocks
	_, blocks, _, err := core.GenerateChainWithGenesis(gspec, engine, numBlocks, ap4.TargetBlockRate-1, genBlocks)
	if err != nil {
		t.Fatal(err)
	}
	// Construct testing chain
	chain, err := core.NewBlockChain(rawdb.NewMemoryDatabase(), core.DefaultCacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
	if err != nil {
		t.Fatalf("Failed to create local chain, %v", err)
	}
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("Failed to insert chain, %v", err)
	}
	return &testBackend{chain: chain}
}

func (b *testBackend) CurrentHeader() *types.Header {
	return b.chain.CurrentHeader()
}

func (b *testBackend) LastAcceptedBlock() *types.Block {
	current := b.chain.CurrentBlock()
	if current == nil {
		return nil
	}
	return b.chain.GetBlockByNumber(current.Number.Uint64())
}

func (b *testBackend) GetBlockByNumber(number uint64) *types.Block {
	return b.chain.GetBlockByNumber(number)
}

type suggestTipCapTest struct {
	numBlocks   int
	genBlock    func(i int, b *core.BlockGen)
	expectedTip *big.Int
}

func defaultOracleConfig() Config {
	return Config{
		Blocks:             20,
		Percentile:         60,
		MaxLookbackSeconds: 80,
	}
}

// timeCrunchOracleConfig returns a config with [MaxLookbackSeconds] set to 5
// to ensure that during gas price estimation, we will hit the time based look back limit
func timeCrunchOracleConfig() Config {
	return Config{
		Blocks:             20,
		Percentile:         60,
		MaxLookbackSeconds: 5,
	}
}

func applyGasPriceTest(t *testing.T, test suggestTipCapTest, config Config) {
	if test.genBlock == nil {
		test.genBlock = func(i int, b *core.BlockGen) {}
	}
	backend := newTestBackend(t, test.numBlocks, test.genBlock)
	oracle, err := NewOracle(backend, config)
	require.NoError(t, err)

	// mock time to be consistent across different CI runs
	// sets currentTime to be 20 seconds
	oracle.clock.Set(time.Unix(20, 0))

	got, err := oracle.SuggestTipCap(context.Background())
	backend.teardown()
	require.NoError(t, err)

	if got.Cmp(test.expectedTip) != 0 {
		t.Fatalf("Expected tip (%d), got tip (%d)", test.expectedTip, got)
	}
}

func testGenBlock(t *testing.T, tip int64, numTx int) func(int, *core.BlockGen) {
	return func(i int, b *core.BlockGen) {
		b.SetCoinbase(common.Address{1})

		txTip := big.NewInt(tip * params.GWei)
		signer := types.LatestSigner(params.TestChainConfig)
		baseFee := b.BaseFee()
		feeCap := new(big.Int).Add(baseFee, txTip)
		for j := 0; j < numTx; j++ {
			tx := types.NewTx(&types.DynamicFeeTx{
				ChainID:   params.TestChainConfig.ChainID,
				Nonce:     b.TxNonce(addr),
				To:        &common.Address{},
				Gas:       ethparams.TxGas,
				GasFeeCap: feeCap,
				GasTipCap: txTip,
				Data:      []byte{},
			})
			tx, err := types.SignTx(tx, signer, key)
			require.NoError(t, err, "failed to create tx")
			b.AddTx(tx)
		}
	}
}

func testGenBlockWithTips(t *testing.T, tips []int64) func(int, *core.BlockGen) {
	return func(i int, b *core.BlockGen) {
		b.SetCoinbase(common.Address{1})
		numTx := len(tips)
		signer := types.LatestSigner(params.TestChainConfig)
		baseFee := b.BaseFee()
		for j := 0; j < numTx; j++ {
			txTip := big.NewInt(tips[j] * params.GWei)
			feeCap := new(big.Int).Add(baseFee, txTip)
			tx := types.NewTx(&types.DynamicFeeTx{
				ChainID:   params.TestChainConfig.ChainID,
				Nonce:     b.TxNonce(addr),
				To:        &common.Address{},
				Gas:       ethparams.TxGas,
				GasFeeCap: feeCap,
				GasTipCap: txTip,
				Data:      []byte{},
			})
			tx, err := types.SignTx(tx, signer, key)
			require.NoError(t, err, "failed to create tx")
			b.AddTx(tx)
		}
	}
}

func TestSuggestTipCap(t *testing.T) {
	cases := []struct {
		name        string
		numBlocks   int
		genBlock    func(int, *core.BlockGen)
		expectedTip *big.Int
	}{
		{
			name:        "simple_latest_no_tip",
			numBlocks:   3,
			genBlock:    testGenBlock(t, 0, 80),
			expectedTip: DefaultMinPrice,
		},
		{
			name:        "simple_latest_1_gwei_tip",
			numBlocks:   3,
			genBlock:    testGenBlock(t, 1, 80),
			expectedTip: big.NewInt(1 * params.GWei),
		},
		{
			name:        "simple_latest_100_gwei_tip",
			numBlocks:   3,
			genBlock:    testGenBlock(t, 100, 80),
			expectedTip: big.NewInt(100 * params.GWei),
		},
		{
			name:        "simple_floor_latest_1_gwei_tip",
			numBlocks:   3,
			genBlock:    testGenBlock(t, 1, 80),
			expectedTip: big.NewInt(1 * params.GWei),
		},
		{
			name:        "simple_floor_latest_100_gwei_tip",
			numBlocks:   3,
			genBlock:    testGenBlock(t, 100, 80),
			expectedTip: big.NewInt(100 * params.GWei),
		},
		{
			name:        "max_tip_cap",
			numBlocks:   200,
			genBlock:    testGenBlock(t, 550, 80),
			expectedTip: DefaultMaxPrice,
		},
		{
			name:        "single_transaction_with_tip",
			numBlocks:   3,
			genBlock:    testGenBlockWithTips(t, []int64{100}),
			expectedTip: big.NewInt(100 * params.GWei),
		},
		{
			name:        "three_transactions_with_odd_count_tips",
			numBlocks:   3,
			genBlock:    testGenBlockWithTips(t, []int64{10, 20, 30}),
			expectedTip: big.NewInt(20 * params.GWei),
		},
		{
			name:        "four_transactions_with_even_count_tips",
			numBlocks:   3,
			genBlock:    testGenBlockWithTips(t, []int64{10, 20, 30, 40}),
			expectedTip: big.NewInt(30 * params.GWei),
		},
		{
			name:        "unsorted_transactions_with_tips",
			numBlocks:   3,
			genBlock:    testGenBlockWithTips(t, []int64{50, 10, 40, 30, 20}),
			expectedTip: big.NewInt(30 * params.GWei),
		},
		{
			name:        "zero_tips",
			numBlocks:   3,
			genBlock:    testGenBlockWithTips(t, []int64{0, 0, 0}),
			expectedTip: DefaultMinPrice,
		},
		{
			name:        "duplicate_tips",
			numBlocks:   3,
			genBlock:    testGenBlockWithTips(t, []int64{20, 20, 20}),
			expectedTip: big.NewInt(20 * params.GWei),
		},
		{
			name:      "no_transactions",
			numBlocks: 3,
			genBlock: func(i int, b *core.BlockGen) {
				b.SetCoinbase(common.Address{1})
				// No transactions added
			},
			expectedTip: DefaultMinPrice,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			applyGasPriceTest(t, suggestTipCapTest{
				numBlocks:   c.numBlocks,
				genBlock:    c.genBlock,
				expectedTip: c.expectedTip,
			}, defaultOracleConfig())
		})
	}
}

func TestSuggestTipCapMaxBlocksSecondsLookback(t *testing.T) {
	applyGasPriceTest(t, suggestTipCapTest{
		numBlocks:   20,
		genBlock:    testGenBlock(t, 55, 80),
		expectedTip: big.NewInt(55 * params.GWei),
	}, timeCrunchOracleConfig())
}
