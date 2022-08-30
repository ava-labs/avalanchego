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

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/consensus/dummy"
	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/core/vm"
	"github.com/ava-labs/subnet-evm/ethdb"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile"
	"github.com/ava-labs/subnet-evm/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/stretchr/testify/require"
)

const testHead = 32

var (
	key, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	addr   = crypto.PubkeyToAddress(key.PublicKey)
	bal, _ = new(big.Int).SetString("100000000000000000000000", 10)
)

type testBackend struct {
	db      ethdb.Database
	chain   *core.BlockChain
	pending bool // pending block available
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

func (b *testBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return b.chain.GetReceiptsByHash(hash), nil
}

func (b *testBackend) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	if b.pending {
		block := b.chain.GetBlockByNumber(testHead + 1)
		return block, b.chain.GetReceiptsByHash(block.Hash())
	}
	return nil, nil
}

func (b *testBackend) ChainConfig() *params.ChainConfig {
	return b.chain.Config()
}

func (b *testBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return nil
}

func (b *testBackend) GetFeeConfigAt(parent *types.Header) (commontype.FeeConfig, *big.Int, error) {
	return b.chain.GetFeeConfigAt(parent)
}

func newTestBackendFakerEngine(t *testing.T, config *params.ChainConfig, numBlocks int, genBlocks func(i int, b *core.BlockGen)) *testBackend {
	gspec := &core.Genesis{
		Config: config,
		Alloc:  core.GenesisAlloc{addr: core.GenesisAccount{Balance: bal}},
	}

	engine := dummy.NewETHFaker()
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

func newTestBackend(t *testing.T, config *params.ChainConfig, numBlocks int, genBlocks func(i int, b *core.BlockGen)) *testBackend {
	gspec := &core.Genesis{
		Config: config,
		Alloc:  core.GenesisAlloc{addr: core.GenesisAccount{Balance: bal}},
	}

	engine := dummy.NewFaker()
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
	return &testBackend{chain: chain, db: db}
}

func (b *testBackend) MinRequiredTip(ctx context.Context, header *types.Header) (*big.Int, error) {
	return dummy.MinRequiredTip(b.chain.Config(), header)
}

func (b *testBackend) CurrentHeader() *types.Header {
	return b.chain.CurrentHeader()
}

func (b *testBackend) LastAcceptedBlock() *types.Block {
	return b.chain.CurrentBlock()
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
		Blocks:     20,
		Percentile: 60,
	}

	if test.genBlock == nil {
		test.genBlock = func(i int, b *core.BlockGen) {}
	}
	backend := newTestBackend(t, test.chainConfig, test.numBlocks, test.genBlock)
	oracle, err := NewOracle(backend, config)
	if err != nil {
		t.Fatal(err)
	}

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
		"subnet evm": {
			chainConfig: params.TestChainConfig,
			expectedTip: DefaultMinPrice,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			applyGasPriceTest(t, test)
		})
	}
}

func TestSuggestTipCap(t *testing.T) {
	txTip := big.NewInt(55 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig: params.TestChainConfig,
		numBlocks:   3,
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
		expectedTip: big.NewInt(1_287_001_287),
	})
}

func TestSuggestTipCapSimple(t *testing.T) {
	txTip := big.NewInt(55 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig: params.TestChainConfig,
		numBlocks:   3,
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
		expectedTip: big.NewInt(1_287_001_287),
	})
}

func TestSuggestTipCapSimpleFloor(t *testing.T) {
	txTip := big.NewInt(55 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig: params.TestChainConfig,
		numBlocks:   1,
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
		chainConfig: params.TestChainConfig,
		numBlocks:   3,
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
		expectedTip: big.NewInt(1_287_001_287),
	})
}

func TestSuggestTipCapMinGas(t *testing.T) {
	txTip := big.NewInt(500 * params.GWei)
	applyGasPriceTest(t, suggestTipCapTest{
		chainConfig: params.TestChainConfig,
		numBlocks:   3,
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

// Regression test to ensure that SuggestPrice does not panic prior to activation of Subnet EVM
// Note: support for gas estimation without activated hard forks has been deprecated, but we still
// ensure that the call does not panic.
func TestSuggestGasPricePreSubnetEVM(t *testing.T) {
	config := Config{
		Blocks:     20,
		Percentile: 60,
	}

	backend := newTestBackend(t, params.TestPreSubnetEVMConfig, 3, func(i int, b *core.BlockGen) {
		b.SetCoinbase(common.Address{1})

		signer := types.LatestSigner(params.TestPreSubnetEVMConfig)
		gasPrice := big.NewInt(params.MinGasPrice)
		for j := 0; j < 50; j++ {
			tx := types.NewTx(&types.LegacyTx{
				Nonce:    b.TxNonce(addr),
				To:       &common.Address{},
				Gas:      params.TxGas,
				GasPrice: gasPrice,
				Data:     []byte{},
			})
			tx, err := types.SignTx(tx, signer, key)
			if err != nil {
				t.Fatalf("failed to create tx: %s", err)
			}
			b.AddTx(tx)
		}
	})
	oracle, err := NewOracle(backend, config)
	if err != nil {
		t.Fatal(err)
	}

	_, err = oracle.SuggestPrice(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// Regression test to ensure that SuggestPrice does not panic prior to activation of SubnetEVM
// Note: support for gas estimation without activated hard forks has been deprecated, but we still
// ensure that the call does not panic.
func TestSuggestGasPricePreAP3(t *testing.T) {
	config := Config{
		Blocks:     20,
		Percentile: 60,
	}

	backend := newTestBackend(t, params.TestChainConfig, 3, func(i int, b *core.BlockGen) {
		b.SetCoinbase(common.Address{1})

		signer := types.LatestSigner(params.TestChainConfig)
		gasPrice := big.NewInt(params.MinGasPrice)
		for j := 0; j < 50; j++ {
			tx := types.NewTx(&types.LegacyTx{
				Nonce:    b.TxNonce(addr),
				To:       &common.Address{},
				Gas:      params.TxGas,
				GasPrice: gasPrice,
				Data:     []byte{},
			})
			tx, err := types.SignTx(tx, signer, key)
			if err != nil {
				t.Fatalf("failed to create tx: %s", err)
			}
			b.AddTx(tx)
		}
	})
	oracle, err := NewOracle(backend, config)
	if err != nil {
		t.Fatal(err)
	}

	_, err = oracle.SuggestPrice(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// Regression test to ensure the last estimation of base fee is not used
// for the block immediately following a fee configuration update.
func TestSuggestGasPriceAfterFeeConfigUpdate(t *testing.T) {
	require := require.New(t)
	config := Config{
		Blocks:     20,
		Percentile: 60,
	}

	// create a chain config with fee manager enabled at genesis with [addr] as the admin
	chainConfig := *params.TestChainConfig
	chainConfig.FeeManagerConfig = precompile.NewFeeManagerConfig(big.NewInt(0), []common.Address{addr})

	// create a fee config with higher MinBaseFee and prepare it for inclusion in a tx
	signer := types.LatestSigner(params.TestChainConfig)
	highFeeConfig := chainConfig.FeeConfig
	highFeeConfig.MinBaseFee = big.NewInt(28_000_000_000)
	data, err := precompile.PackSetFeeConfig(highFeeConfig)
	require.NoError(err)

	// before issuing the block changing the fee into the chain, the fee estimation should
	// follow the fee config in genesis.
	backend := newTestBackend(t, &chainConfig, 0, func(i int, b *core.BlockGen) {})
	oracle, err := NewOracle(backend, config)
	require.NoError(err)
	got, err := oracle.SuggestPrice(context.Background())
	require.NoError(err)
	require.Equal(chainConfig.FeeConfig.MinBaseFee, got)

	// issue the block with tx that changes the fee
	genesis := backend.chain.Genesis()
	engine := backend.chain.Engine()
	blocks, _, err := core.GenerateChain(&chainConfig, genesis, engine, backend.db, 1, 0, func(i int, b *core.BlockGen) {
		b.SetCoinbase(common.Address{1})

		// admin issues tx to change fee config to higher MinBaseFee
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainConfig.ChainID,
			Nonce:     b.TxNonce(addr),
			To:        &precompile.FeeConfigManagerAddress,
			Gas:       chainConfig.FeeConfig.GasLimit.Uint64(),
			Value:     common.Big0,
			GasFeeCap: chainConfig.FeeConfig.MinBaseFee, // give low fee, it should work since we still haven't applied high fees
			GasTipCap: common.Big0,
			Data:      data,
		})
		tx, err = types.SignTx(tx, signer, key)
		require.NoError(err, "failed to create tx")
		b.AddTx(tx)
	})
	require.NoError(err)
	_, err = backend.chain.InsertChain(blocks)
	require.NoError(err)

	// verify the suggested price follows the new fee config.
	got, err = oracle.SuggestPrice(context.Background())
	require.NoError(err)
	require.Equal(highFeeConfig.MinBaseFee, got)
}
