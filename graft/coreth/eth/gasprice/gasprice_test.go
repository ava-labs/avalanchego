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

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
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

func newTestBackend(t *testing.T, config *params.ChainConfig, numBlocks int, extDataGasUsage *big.Int, genBlocks func(i int, b *core.BlockGen)) *testBackend {
	var gspec = &core.Genesis{
		Config: config,
		Alloc:  core.GenesisAlloc{addr: core.GenesisAccount{Balance: bal}},
	}

	engine := dummy.NewDummyEngine(&dummy.ConsensusCallbacks{
		OnFinalizeAndAssemble: func(header *types.Header, state *state.StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
			return nil, common.Big0, extDataGasUsage, nil
		},
		OnExtraStateChange: func(block *types.Block, state *state.StateDB) (*big.Int, *big.Int, error) {
			return common.Big0, extDataGasUsage, nil
		},
	})
	db := rawdb.NewMemoryDatabase()
	genesis := gspec.MustCommit(db)

	// Generate testing blocks
	blocks, _, err := core.GenerateChain(gspec.Config, genesis, engine, db, numBlocks, 0, genBlocks)
	if err != nil {
		t.Fatal(err)
	}
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

func (b *testBackend) MinRequiredTip(ctx context.Context, header *types.Header) (*big.Int, error) {
	return dummy.MinRequiredTip(b.chain.Config(), header)
}

func (b *testBackend) CurrentHeader() *types.Header {
	return b.chain.CurrentHeader()
}

func (b *testBackend) GetBlockByNumber(number uint64) *types.Block {
	return b.chain.GetBlockByNumber(number)
}

type suggestTipCapTest struct {
	chainConfig     *params.ChainConfig
	numBlocks       int
	extDataGasUsage *big.Int
	genBlock        func(i int, b *core.BlockGen)
	expectedTip     *big.Int
}

func applyGasPriceTest(t *testing.T, test suggestTipCapTest) {
	config := Config{
		Blocks:     20,
		Percentile: 60,
	}

	if test.genBlock == nil {
		test.genBlock = func(i int, b *core.BlockGen) {}
	}
	backend := newTestBackend(t, test.chainConfig, test.numBlocks, test.extDataGasUsage, test.genBlock)
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
			chainConfig: params.TestApricotPhase4Config,
			expectedTip: DefaultMinPrice,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			applyGasPriceTest(t, test)
		})
	}
}

func TestSuggestTipCapEmptyExtDataGasUsage(t *testing.T) {
	txTip := big.NewInt(55 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig:     params.TestChainConfig,
		numBlocks:       3,
		extDataGasUsage: nil,
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			signer := types.LatestSigner(params.TestChainConfig)
			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, txTip)
			for j := 0; j < 370; j++ {
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: txTip,
					Data:      []byte{},
				})
				tx, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
			}
		},
		expectedTip: big.NewInt(2_844_353_281),
	})
}

func TestSuggestTipCapSimple(t *testing.T) {
	txTip := big.NewInt(55 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig:     params.TestChainConfig,
		numBlocks:       3,
		extDataGasUsage: common.Big0,
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			signer := types.LatestSigner(params.TestChainConfig)
			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, txTip)
			for j := 0; j < 370; j++ {
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: txTip,
					Data:      []byte{},
				})
				tx, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
			}
		},
		expectedTip: big.NewInt(2_844_353_281),
	})
}

func TestSuggestTipCapSimpleFloor(t *testing.T) {
	txTip := big.NewInt(55 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig:     params.TestChainConfig,
		numBlocks:       1,
		extDataGasUsage: common.Big0,
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			signer := types.LatestSigner(params.TestChainConfig)
			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, txTip)
			for j := 0; j < 370; j++ {
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: txTip,
					Data:      []byte{},
				})
				tx, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
			}
		},
		expectedTip: common.Big0,
	})
}

func TestSuggestTipCapSmallTips(t *testing.T) {
	tip := big.NewInt(550 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig:     params.TestChainConfig,
		numBlocks:       3,
		extDataGasUsage: common.Big0,
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			signer := types.LatestSigner(params.TestChainConfig)
			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, tip)
			for j := 0; j < 185; j++ {
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
		// NOTE: small tips do not bias estimate
		expectedTip: big.NewInt(2_844_353_281),
	})
}

func TestSuggestTipCapExtDataUsage(t *testing.T) {
	txTip := big.NewInt(55 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig:     params.TestChainConfig,
		numBlocks:       3,
		extDataGasUsage: big.NewInt(10_000),
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			signer := types.LatestSigner(params.TestChainConfig)
			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, txTip)
			for j := 0; j < 370; j++ {
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: txTip,
					Data:      []byte{},
				})
				tx, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
			}
		},
		expectedTip: big.NewInt(2_840_938_303),
	})
}

func TestSuggestTipCapMinGas(t *testing.T) {
	txTip := big.NewInt(55 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig:     params.TestChainConfig,
		numBlocks:       3,
		extDataGasUsage: common.Big0,
		genBlock: func(i int, b *core.BlockGen) {
			b.SetCoinbase(common.Address{1})

			signer := types.LatestSigner(params.TestChainConfig)
			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, txTip)
			for j := 0; j < 50; j++ {
				tx := types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.TestChainConfig.ChainID,
					Nonce:     b.TxNonce(addr),
					To:        &common.Address{},
					Gas:       params.TxGas,
					GasFeeCap: feeCap,
					GasTipCap: txTip,
					Data:      []byte{},
				})
				tx, err := types.SignTx(tx, signer, key)
				if err != nil {
					t.Fatalf("failed to create tx: %s", err)
				}
				b.AddTx(tx)
			}
		},
		expectedTip: big.NewInt(0),
	})
}
