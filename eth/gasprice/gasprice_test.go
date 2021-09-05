// (c) 2019-2020, Ava Labs, Inc.
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
	"testing"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	key, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	addr   = crypto.PubkeyToAddress(key.PublicKey)
	bal, _ = new(big.Int).SetString("100000000000000000000000", 10)
)

type testBackend struct {
	chain *core.BlockChain
}

func (b *testBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	if number == rpc.LatestBlockNumber {
		return b.chain.CurrentBlock().Header(), nil
	}
	return b.chain.GetHeaderByNumber(uint64(number)), nil
}

func (b *testBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	if number == rpc.LatestBlockNumber {
		return b.chain.CurrentBlock(), nil
	}
	return b.chain.GetBlockByNumber(uint64(number)), nil
}

func (b *testBackend) ChainConfig() *params.ChainConfig {
	return b.chain.Config()
}

func newTestBackend(t *testing.T, config *params.ChainConfig, numBlocks int, genBlocks func(i int, b *core.BlockGen)) *testBackend {
	var gspec = &core.Genesis{
		Config: config,
		Alloc:  core.GenesisAlloc{addr: core.GenesisAccount{Balance: bal}},
	}

	engine := dummy.NewFakerSkipBlockFee()
	db := rawdb.NewMemoryDatabase()
	genesis := gspec.MustCommit(db)

	// Generate testing blocks
	blocks, _ := core.GenerateChain(gspec.Config, genesis, engine, db, numBlocks, genBlocks)
	// Construct testing chain
	diskdb := rawdb.NewMemoryDatabase()
	gspec.Commit(diskdb)
	chain, err := core.NewBlockChain(diskdb, core.DefaultCacheConfig, gspec.Config, engine, vm.Config{}, common.Hash{})
	if err != nil {
		t.Fatalf("Failed to create local chain, %v", err)
	}
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("Failed to insert chain, %v", err)
	}
	return &testBackend{chain: chain}
}

func (b *testBackend) Engine() consensus.Engine {
	return b.chain.Engine()
}

func (b *testBackend) MinRequiredTip(ctx context.Context, block *types.Block) (*big.Int, error) {
	return big.NewInt(0), nil
}

func (b *testBackend) CurrentHeader() *types.Header {
	return b.chain.CurrentHeader()
}

func (b *testBackend) GetBlockByNumber(number uint64) *types.Block {
	return b.chain.GetBlockByNumber(number)
}

type suggestTipCapTest struct {
	chainConfig *params.ChainConfig
	numBlocks   int
	genBlock    func(i int, b *core.BlockGen)
	expectedTip *big.Int
}

func applyGasPriceTest(t *testing.T, test suggestTipCapTest) {
	config := Config{
		Blocks:     3,
		Percentile: 60,
	}

	if test.genBlock == nil {
		test.genBlock = func(i int, b *core.BlockGen) {}
	}
	backend := newTestBackend(t, test.chainConfig, test.numBlocks, test.genBlock)
	oracle := NewOracle(backend, config)

	got, err := oracle.SuggestTipCap(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Cmp(test.expectedTip) != 0 {
		t.Fatalf("Expected tip (%d), got tip (%d)", test.expectedTip, got)
	}
}

func TestSuggestTipCapNetworkUpgrades(t *testing.T) {
	tests := map[string]suggestTipCapTest{
		"launch": {
			chainConfig: params.TestLaunchConfig,
			expectedTip: big.NewInt(params.LaunchMinGasPrice),
		},
		"apricot phase 1": {
			chainConfig: params.TestApricotPhase1Config,
			expectedTip: big.NewInt(params.ApricotPhase1MinGasPrice),
		},
		"apricot phase 2": {
			chainConfig: params.TestApricotPhase2Config,
			expectedTip: big.NewInt(params.ApricotPhase1MinGasPrice),
		},
		"apricot phase 3": {
			chainConfig: params.TestApricotPhase3Config,
			expectedTip: big.NewInt(0),
		},
		"apricot phase 4": {
			chainConfig: params.TestChainConfig,
			expectedTip: big.NewInt(0),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			applyGasPriceTest(t, test)
		})
	}
}

func TestSuggestTipCapIdenticalTip(t *testing.T) {
	tip := big.NewInt(10 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig: params.TestChainConfig,
		numBlocks:   3,
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			signer := types.LatestSigner(params.TestChainConfig)
			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, tip)
			// Need to add 50 transactions, so that the block consumes more than the skip block
			// gas limit
			for j := 0; j < 50; j++ {
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: tip,
					Data:      []byte{},
				})
				tx, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
			}
		},
		expectedTip: tip,
	})
}

func TestSuggestTipCapDifferentTips(t *testing.T) {
	tip := big.NewInt(10 * params.GWei)
	signer := types.LatestSigner(params.TestChainConfig)

	highTip := new(big.Int).Add(tip, common.Big1)
	lowTip := new(big.Int).Sub(tip, common.Big1)

	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig: params.TestChainConfig,
		numBlocks:   3,
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, highTip)
			// Need to add 50 transactions, so that the block consumes more than the skip block
			// gas limit
			// Add 50 transactions each at the low and high tip amount. This should result in the high tip
			// being selected, since we select the 60th percentile.
			for j := 0; j < 50; j++ {
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: highTip,
					Data:      []byte{},
				})
				tx, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
				tx = types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: lowTip,
					Data:      []byte{},
				})
				tx, err = types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
			}
		},
		expectedTip: highTip,
	})
}

func TestSuggestTipCapIgnoreSmallTips(t *testing.T) {
	tip := big.NewInt(10 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig: params.TestChainConfig,
		numBlocks:   3,
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			signer := types.LatestSigner(params.TestChainConfig)
			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, tip)
			// Need to add 50 transactions, so that the block consumes more than the skip block
			// gas limit
			for j := 0; j < 25; j++ {
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: tip,
					Data:      []byte{},
				})
				tx, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
				tx = types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: common.Big1,
					Data:      []byte{},
				})
				tx, err = types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
			}
		},
		expectedTip: tip,
	})
}
func TestSuggestTipcapLegacyTxs(t *testing.T) {
	tip := big.NewInt(10 * params.GWei)

	// what should we do if we reach further than 20s back when retrieving block values
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig: params.TestChainConfig,
		numBlocks:   3,
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			signer := types.LatestSigner(params.TestChainConfig)
			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, tip)
			// Need to add 50 transactions, so that the block consumes more than the skip block
			// gas limit
			for j := 0; j < 50; j++ {
				tx := types.NewTx(&types.LegacyTx{
					Nonce:    b.TxNonce(addr),
					To:       &common.Address{},
					Gas:      params.TxGas,
					GasPrice: feeCap,
					Data:     []byte{},
				})
				tx, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
			}
		},
		expectedTip: tip,
	})
}
