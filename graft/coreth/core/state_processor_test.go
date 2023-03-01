// (c) 2019-2021, Ava Labs, Inc.
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

package core

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/sha3"
)

var (
	config     = params.TestChainConfig
	signer     = types.LatestSigner(config)
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddr   = common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7")
)

func makeTx(nonce uint64, to common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *types.Transaction {
	tx, _ := types.SignTx(types.NewTransaction(nonce, to, amount, gasLimit, gasPrice, data), signer, testKey)
	return tx
}

func makeContractTx(nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *types.Transaction {
	tx, _ := types.SignTx(types.NewContractCreation(nonce, amount, gasLimit, gasPrice, data), signer, testKey)
	return tx
}

func mkDynamicTx(nonce uint64, to common.Address, gasLimit uint64, gasTipCap, gasFeeCap *big.Int) *types.Transaction {
	tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       gasLimit,
		To:        &to,
		Value:     big.NewInt(0),
	}), signer, testKey)
	return tx
}

func mkDynamicCreationTx(nonce uint64, gasLimit uint64, gasTipCap, gasFeeCap *big.Int, data []byte) *types.Transaction {
	tx, _ := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		Nonce:     nonce,
		GasTipCap: gasTipCap,
		GasFeeCap: gasFeeCap,
		Gas:       gasLimit,
		Value:     big.NewInt(0),
		Data:      data,
	}), signer, testKey)
	return tx
}

// TestStateProcessorErrors tests the output from the 'core' errors
// as defined in core/error.go. These errors are generated when the
// blockchain imports bad blocks, meaning blocks which have valid headers but
// contain invalid transactions
func TestStateProcessorErrors(t *testing.T) {
	{ // Tests against a 'recent' chain definition
		var (
			db    = rawdb.NewMemoryDatabase()
			gspec = &Genesis{
				Config: config,
				Alloc: GenesisAlloc{
					common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"): GenesisAccount{
						Balance: big.NewInt(4000000000000000000), // 4 ether
						Nonce:   0,
					},
				},
				GasLimit: params.CortinaGasLimit,
			}
			genesis       = gspec.MustCommit(db)
			blockchain, _ = NewBlockChain(db, DefaultCacheConfig, gspec.Config, dummy.NewFaker(), vm.Config{}, common.Hash{})
		)
		defer blockchain.Stop()
		bigNumber := new(big.Int).SetBytes(common.FromHex("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"))
		tooBigNumber := new(big.Int).Set(bigNumber)
		tooBigNumber.Add(tooBigNumber, common.Big1)
		for i, tt := range []struct {
			txs  []*types.Transaction
			want string
		}{
			{ // ErrNonceTooLow
				txs: []*types.Transaction{
					makeTx(0, common.Address{}, big.NewInt(0), params.TxGas, big.NewInt(225000000000), nil),
					makeTx(0, common.Address{}, big.NewInt(0), params.TxGas, big.NewInt(225000000000), nil),
				},
				want: "could not apply tx 1 [0x734d821c990099c6ae42d78072aadd3931c35328cf03ef4cf5b2a4ac9c398522]: nonce too low: address 0x71562b71999873DB5b286dF957af199Ec94617F7, tx: 0 state: 1",
			},
			{ // ErrNonceTooHigh
				txs: []*types.Transaction{
					makeTx(100, common.Address{}, big.NewInt(0), params.TxGas, big.NewInt(225000000000), nil),
				},
				want: "could not apply tx 0 [0x0df36254cfbef8ed6961b38fc68aecc777177166144c8a56bc8919e23a559bf4]: nonce too high: address 0x71562b71999873DB5b286dF957af199Ec94617F7, tx: 100 state: 0",
			},
			{ // ErrGasLimitReached
				txs: []*types.Transaction{
					makeTx(0, common.Address{}, big.NewInt(0), 15000001, big.NewInt(225000000000), nil),
				},
				want: "could not apply tx 0 [0x1354370681d2ab68247073d889736f8be4a8d87e35956f0c02658d3670803a66]: gas limit reached",
			},
			{ // ErrInsufficientFundsForTransfer
				txs: []*types.Transaction{
					makeTx(0, common.Address{}, big.NewInt(4000000000000000000), params.TxGas, big.NewInt(225000000000), nil),
				},
				want: "could not apply tx 0 [0x1632f2bffcce84a5c91dd8ab2016128fccdbcfbe0485d2c67457e1c793c72a4b]: insufficient funds for gas * price + value: address 0x71562b71999873DB5b286dF957af199Ec94617F7 have 4000000000000000000 want 4004725000000000000",
			},
			{ // ErrInsufficientFunds
				txs: []*types.Transaction{
					makeTx(0, common.Address{}, big.NewInt(0), params.TxGas, big.NewInt(900000000000000000), nil),
				},
				want: "could not apply tx 0 [0x4a69690c4b0cd85e64d0d9ea06302455b01e10a83db964d60281739752003440]: insufficient funds for gas * price + value: address 0x71562b71999873DB5b286dF957af199Ec94617F7 have 4000000000000000000 want 18900000000000000000000",
			},
			// ErrGasUintOverflow
			// One missing 'core' error is ErrGasUintOverflow: "gas uint64 overflow",
			// In order to trigger that one, we'd have to allocate a _huge_ chunk of data, such that the
			// multiplication len(data) +gas_per_byte overflows uint64. Not testable at the moment
			{ // ErrIntrinsicGas
				txs: []*types.Transaction{
					makeTx(0, common.Address{}, big.NewInt(0), params.TxGas-1000, big.NewInt(225000000000), nil),
				},
				want: "could not apply tx 0 [0x2fc3e3b5cc26917d413e26983fe189475f47d4f0757e32aaa5561fcb9c9dc432]: intrinsic gas too low: have 20000, want 21000",
			},
			{ // ErrGasLimitReached
				txs: []*types.Transaction{
					makeTx(0, common.Address{}, big.NewInt(0), params.TxGas*762, big.NewInt(225000000000), nil),
				},
				want: "could not apply tx 0 [0x76c07cc2b32007eb1a9c3fa066d579a3d77ec4ecb79bbc266624a601d7b08e46]: gas limit reached",
			},
			{ // ErrFeeCapTooLow
				txs: []*types.Transaction{
					mkDynamicTx(0, common.Address{}, params.TxGas, big.NewInt(0), big.NewInt(0)),
				},
				want: "could not apply tx 0 [0xc4ab868fef0c82ae0387b742aee87907f2d0fc528fc6ea0a021459fb0fc4a4a8]: max fee per gas less than block base fee: address 0x71562b71999873DB5b286dF957af199Ec94617F7, maxFeePerGas: 0 baseFee: 225000000000",
			},
			{ // ErrTipVeryHigh
				txs: []*types.Transaction{
					mkDynamicTx(0, common.Address{}, params.TxGas, tooBigNumber, big.NewInt(1)),
				},
				want: "could not apply tx 0 [0x15b8391b9981f266b32f3ab7da564bbeb3d6c21628364ea9b32a21139f89f712]: max priority fee per gas higher than 2^256-1: address 0x71562b71999873DB5b286dF957af199Ec94617F7, maxPriorityFeePerGas bit length: 257",
			},
			{ // ErrFeeCapVeryHigh
				txs: []*types.Transaction{
					mkDynamicTx(0, common.Address{}, params.TxGas, big.NewInt(1), tooBigNumber),
				},
				want: "could not apply tx 0 [0x48bc299b83fdb345c57478f239e89814bb3063eb4e4b49f3b6057a69255c16bd]: max fee per gas higher than 2^256-1: address 0x71562b71999873DB5b286dF957af199Ec94617F7, maxFeePerGas bit length: 257",
			},
			{ // ErrTipAboveFeeCap
				txs: []*types.Transaction{
					mkDynamicTx(0, common.Address{}, params.TxGas, big.NewInt(2), big.NewInt(1)),
				},
				want: "could not apply tx 0 [0xf987a31ff0c71895780a7612f965a0c8b056deb54e020bb44fa478092f14c9b4]: max priority fee per gas higher than max fee per gas: address 0x71562b71999873DB5b286dF957af199Ec94617F7, maxPriorityFeePerGas: 2, maxFeePerGas: 1",
			},
			{ // ErrInsufficientFunds
				// Available balance:           1000000000000000000
				// Effective cost:                   18375000021000
				// FeeCap * gas:                1050000000000000000
				// This test is designed to have the effective cost be covered by the balance, but
				// the extended requirement on FeeCap*gas < balance to fail
				txs: []*types.Transaction{
					mkDynamicTx(0, common.Address{}, params.TxGas, big.NewInt(1), big.NewInt(200000000000000)),
				},
				want: "could not apply tx 0 [0xa3840aa3cad37eec8607b9f4846813d4a80e70b462a793fa21f64138156f849b]: insufficient funds for gas * price + value: address 0x71562b71999873DB5b286dF957af199Ec94617F7 have 4000000000000000000 want 4200000000000000000",
			},
			{ // Another ErrInsufficientFunds, this one to ensure that feecap/tip of max u256 is allowed
				txs: []*types.Transaction{
					mkDynamicTx(0, common.Address{}, params.TxGas, bigNumber, bigNumber),
				},
				want: "could not apply tx 0 [0xd82a0c2519acfeac9a948258c47e784acd20651d9d80f9a1c67b4137651c3a24]: insufficient funds for gas * price + value: address 0x71562b71999873DB5b286dF957af199Ec94617F7 have 4000000000000000000 want 2431633873983640103894990685182446064918669677978451844828609264166175722438635000",
			},
		} {
			block := GenerateBadBlock(genesis, dummy.NewFaker(), tt.txs, gspec.Config)
			_, err := blockchain.InsertChain(types.Blocks{block})
			if err == nil {
				t.Fatal("block imported without errors")
			}
			if have, want := err.Error(), tt.want; have != want {
				t.Errorf("test %d:\nhave \"%v\"\nwant \"%v\"\n", i, have, want)
			}
		}
	}

	// ErrTxTypeNotSupported, For this, we need an older chain
	{
		var (
			db    = rawdb.NewMemoryDatabase()
			gspec = &Genesis{
				Config: &params.ChainConfig{
					ChainID:                     big.NewInt(1),
					HomesteadBlock:              big.NewInt(0),
					EIP150Block:                 big.NewInt(0),
					EIP150Hash:                  common.Hash{},
					EIP155Block:                 big.NewInt(0),
					EIP158Block:                 big.NewInt(0),
					ByzantiumBlock:              big.NewInt(0),
					ConstantinopleBlock:         big.NewInt(0),
					PetersburgBlock:             big.NewInt(0),
					IstanbulBlock:               big.NewInt(0),
					MuirGlacierBlock:            big.NewInt(0),
					ApricotPhase1BlockTimestamp: big.NewInt(0),
					ApricotPhase2BlockTimestamp: big.NewInt(0),
				},
				Alloc: GenesisAlloc{
					common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"): GenesisAccount{
						Balance: big.NewInt(1000000000000000000), // 1 ether
						Nonce:   0,
					},
				},
				GasLimit: params.ApricotPhase1GasLimit,
			}
			genesis       = gspec.MustCommit(db)
			blockchain, _ = NewBlockChain(db, DefaultCacheConfig, gspec.Config, dummy.NewFaker(), vm.Config{}, common.Hash{})
		)
		defer blockchain.Stop()
		for i, tt := range []struct {
			txs  []*types.Transaction
			want string
		}{
			{ // ErrTxTypeNotSupported
				txs: []*types.Transaction{
					mkDynamicTx(0, common.Address{}, params.TxGas-1000, big.NewInt(0), big.NewInt(0)),
				},
				want: "could not apply tx 0 [0x88626ac0d53cb65308f2416103c62bb1f18b805573d4f96a3640bbbfff13c14f]: transaction type not supported",
			},
		} {
			block := GenerateBadBlock(genesis, dummy.NewFaker(), tt.txs, gspec.Config)
			_, err := blockchain.InsertChain(types.Blocks{block})
			if err == nil {
				t.Fatal("block imported without errors")
			}
			if have, want := err.Error(), tt.want; have != want {
				t.Errorf("test %d:\nhave \"%v\"\nwant \"%v\"\n", i, have, want)
			}
		}
	}

	// ErrSenderNoEOA, for this we need the sender to have contract code
	{
		var (
			db    = rawdb.NewMemoryDatabase()
			gspec = &Genesis{
				Config: config,
				Alloc: GenesisAlloc{
					common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"): GenesisAccount{
						Balance: big.NewInt(1000000000000000000), // 1 ether
						Nonce:   0,
						Code:    common.FromHex("0xB0B0FACE"),
					},
				},
				GasLimit: params.CortinaGasLimit,
			}
			genesis       = gspec.MustCommit(db)
			blockchain, _ = NewBlockChain(db, DefaultCacheConfig, gspec.Config, dummy.NewFaker(), vm.Config{}, common.Hash{})
		)
		defer blockchain.Stop()
		for i, tt := range []struct {
			txs  []*types.Transaction
			want string
		}{
			{ // ErrSenderNoEOA
				txs: []*types.Transaction{
					mkDynamicTx(0, common.Address{}, params.TxGas-1000, big.NewInt(0), big.NewInt(0)),
				},
				want: "could not apply tx 0 [0x88626ac0d53cb65308f2416103c62bb1f18b805573d4f96a3640bbbfff13c14f]: sender not an eoa: address 0x71562b71999873DB5b286dF957af199Ec94617F7, codehash: 0x9280914443471259d4570a8661015ae4a5b80186dbc619658fb494bebc3da3d1",
			},
		} {
			block := GenerateBadBlock(genesis, dummy.NewFaker(), tt.txs, gspec.Config)
			_, err := blockchain.InsertChain(types.Blocks{block})
			if err == nil {
				t.Fatal("block imported without errors")
			}
			if have, want := err.Error(), tt.want; have != want {
				t.Errorf("test %d:\nhave \"%v\"\nwant \"%v\"\n", i, have, want)
			}
		}
	}

	// ErrMaxInitCodeSizeExceeded, for this we need extra Shanghai (Cortina/EIP-3860) enabled.
	{
		var (
			db    = rawdb.NewMemoryDatabase()
			gspec = &Genesis{
				Config: &params.ChainConfig{
					ChainID:                         big.NewInt(1),
					HomesteadBlock:                  big.NewInt(0),
					DAOForkBlock:                    big.NewInt(0),
					DAOForkSupport:                  true,
					EIP150Block:                     big.NewInt(0),
					EIP150Hash:                      common.HexToHash("0x2086799aeebeae135c246c65021c82b4e15a2c451340993aacfd2751886514f0"),
					EIP155Block:                     big.NewInt(0),
					EIP158Block:                     big.NewInt(0),
					ByzantiumBlock:                  big.NewInt(0),
					ConstantinopleBlock:             big.NewInt(0),
					PetersburgBlock:                 big.NewInt(0),
					IstanbulBlock:                   big.NewInt(0),
					MuirGlacierBlock:                big.NewInt(0),
					ApricotPhase1BlockTimestamp:     big.NewInt(0),
					ApricotPhase2BlockTimestamp:     big.NewInt(0),
					ApricotPhase3BlockTimestamp:     big.NewInt(0),
					ApricotPhase4BlockTimestamp:     big.NewInt(0),
					ApricotPhase5BlockTimestamp:     big.NewInt(0),
					ApricotPhasePre6BlockTimestamp:  big.NewInt(0),
					ApricotPhase6BlockTimestamp:     big.NewInt(0),
					ApricotPhasePost6BlockTimestamp: big.NewInt(0),
					BanffBlockTimestamp:             big.NewInt(0),
					CortinaBlockTimestamp:           big.NewInt(0),
				},
				Alloc: GenesisAlloc{
					common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"): GenesisAccount{
						Balance: big.NewInt(1000000000000000000), // 1 ether
						Nonce:   0,
					},
				},
				GasLimit: params.CortinaGasLimit,
			}
			genesis        = gspec.MustCommit(db)
			blockchain, _  = NewBlockChain(db, DefaultCacheConfig, gspec.Config, dummy.NewFaker(), vm.Config{}, common.Hash{})
			tooBigInitCode = [params.MaxInitCodeSize + 1]byte{}
			smallInitCode  = [320]byte{}
		)
		defer blockchain.Stop()
		for i, tt := range []struct {
			txs  []*types.Transaction
			want string
		}{
			{ // ErrMaxInitCodeSizeExceeded
				txs: []*types.Transaction{

					mkDynamicCreationTx(0, 500000, common.Big0, big.NewInt(params.ApricotPhase3InitialBaseFee), tooBigInitCode[:]),
				},
				want: "could not apply tx 0 [0x18a05f40f29ff16d5287f6f88b21c9f3c7fbc268f707251144996294552c4cd6]: max initcode size exceeded: code size 49153 limit 49152",
			},
			{ // ErrIntrinsicGas: Not enough gas to cover init code
				txs: []*types.Transaction{
					mkDynamicCreationTx(0, 54299, common.Big0, big.NewInt(params.ApricotPhase3InitialBaseFee), smallInitCode[:]),
				},
				want: "could not apply tx 0 [0x849278f616d51ab56bba399551317213ce7a10e4d9cbc3d14bb663e50cb7ab99]: intrinsic gas too low: have 54299, want 54300",
			},
		} {
			block := GenerateBadBlock(genesis, dummy.NewFaker(), tt.txs, gspec.Config)
			_, err := blockchain.InsertChain(types.Blocks{block})
			if err == nil {
				t.Fatal("block imported without errors")
			}
			if have, want := err.Error(), tt.want; have != want {
				t.Errorf("test %d:\nhave \"%v\"\nwant \"%v\"\n", i, have, want)
			}
		}
	}
}

// GenerateBadBlock constructs a "block" which contains the transactions. The transactions are not expected to be
// valid, and no proper post-state can be made. But from the perspective of the blockchain, the block is sufficiently
// valid to be considered for import:
// - valid pow (fake), ancestry, difficulty, gaslimit etc
func GenerateBadBlock(parent *types.Block, engine consensus.Engine, txs types.Transactions, config *params.ChainConfig) *types.Block {
	header := &types.Header{
		ParentHash: parent.Hash(),
		Coinbase:   parent.Coinbase(),
		Difficulty: engine.CalcDifficulty(&fakeChainReader{config}, parent.Time()+10, &types.Header{
			Number:     parent.Number(),
			Time:       parent.Time(),
			Difficulty: parent.Difficulty(),
			UncleHash:  parent.UncleHash(),
		}),
		GasLimit:  parent.GasLimit(),
		Number:    new(big.Int).Add(parent.Number(), common.Big1),
		Time:      parent.Time() + 10,
		UncleHash: types.EmptyUncleHash,
	}
	if config.IsApricotPhase3(new(big.Int).SetUint64(header.Time)) {
		header.Extra, header.BaseFee, _ = dummy.CalcBaseFee(config, parent.Header(), header.Time)
	}
	if config.IsApricotPhase4(new(big.Int).SetUint64(header.Time)) {
		header.BlockGasCost = big.NewInt(0)
		header.ExtDataGasUsed = big.NewInt(0)
	}
	var receipts []*types.Receipt
	// The post-state result doesn't need to be correct (this is a bad block), but we do need something there
	// Preferably something unique. So let's use a combo of blocknum + txhash
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write(header.Number.Bytes())
	var cumulativeGas uint64
	for _, tx := range txs {
		txh := tx.Hash()
		hasher.Write(txh[:])
		receipt := types.NewReceipt(nil, false, cumulativeGas+tx.Gas())
		receipt.TxHash = tx.Hash()
		receipt.GasUsed = tx.Gas()
		receipts = append(receipts, receipt)
		cumulativeGas += tx.Gas()
	}
	header.Root = common.BytesToHash(hasher.Sum(nil))
	// Assemble and return the final block for sealing
	return types.NewBlock(header, txs, nil, receipts, trie.NewStackTrie(nil), nil, true)
}

func CostOfUsingGasLimitEachBlock(gspec *Genesis) {
	db := rawdb.NewMemoryDatabase()
	genesis := gspec.MustCommit(db)
	totalPaid := big.NewInt(0)
	parent := genesis.Header()
	gasLimit := new(big.Int).SetUint64(gspec.GasLimit)
	totalGasUsed := big.NewInt(0)

	for i := 1; i < 20; i++ {
		header := nextBlock(gspec.Config, parent, gspec.GasLimit)
		baseFee := header.BaseFee
		gasCost := new(big.Int).Mul(baseFee, gasLimit)
		totalGasUsed = new(big.Int).Add(totalGasUsed, gasLimit)
		totalPaid = new(big.Int).Add(totalPaid, gasCost)
		parent = header

		avg := new(big.Int).Div(totalPaid, totalGasUsed)
		fmt.Printf(
			"Number: %d, BaseFee: %vGWei, TotalGasUsed: %d, TotalPaid (Ether): %d, AvgGasPrice: %dGWei\n",
			header.Number,
			new(big.Int).Div(baseFee, big.NewInt(params.GWei)), // baseFee in GWei
			totalGasUsed,
			new(big.Int).Div(totalPaid, big.NewInt(params.Ether)), // totalPaid in Ether
			new(big.Int).Div(avg, big.NewInt(params.GWei)),        // avgGasPrice in GWei
		)
	}
}

func ExampleCostOfUsingGasLimitEachBlock() {
	banff := &Genesis{
		Config: params.TestBanffChainConfig,
		Alloc: GenesisAlloc{
			common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"): GenesisAccount{
				Balance: big.NewInt(4000000000000000000), // 4 ether
				Nonce:   0,
			},
		},
		BaseFee:  big.NewInt(225 * params.GWei),
		GasLimit: params.ApricotPhase1GasLimit,
	}
	cortina := &Genesis{
		Config: params.TestCortinaChainConfig,
		Alloc: GenesisAlloc{
			common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"): GenesisAccount{
				Balance: big.NewInt(4000000000000000000), // 4 ether
				Nonce:   0,
			},
		},
		BaseFee:  big.NewInt(225 * params.GWei),
		GasLimit: params.CortinaGasLimit,
	}
	fmt.Println("----- banff ----")
	CostOfUsingGasLimitEachBlock(banff)
	fmt.Println("----- cortina ----")
	CostOfUsingGasLimitEachBlock(cortina)
	// Output:
	// ----- banff ----
	// Number: 1, BaseFee: 225GWei, TotalGasUsed: 8000000, TotalPaid (Ether): 1, AvgGasPrice: 225GWei
	// Number: 2, BaseFee: 222GWei, TotalGasUsed: 16000000, TotalPaid (Ether): 3, AvgGasPrice: 223GWei
	// Number: 3, BaseFee: 222GWei, TotalGasUsed: 24000000, TotalPaid (Ether): 5, AvgGasPrice: 223GWei
	// Number: 4, BaseFee: 226GWei, TotalGasUsed: 32000000, TotalPaid (Ether): 7, AvgGasPrice: 223GWei
	// Number: 5, BaseFee: 233GWei, TotalGasUsed: 40000000, TotalPaid (Ether): 9, AvgGasPrice: 225GWei
	// Number: 6, BaseFee: 240GWei, TotalGasUsed: 48000000, TotalPaid (Ether): 10, AvgGasPrice: 228GWei
	// Number: 7, BaseFee: 248GWei, TotalGasUsed: 56000000, TotalPaid (Ether): 12, AvgGasPrice: 231GWei
	// Number: 8, BaseFee: 256GWei, TotalGasUsed: 64000000, TotalPaid (Ether): 14, AvgGasPrice: 234GWei
	// Number: 9, BaseFee: 264GWei, TotalGasUsed: 72000000, TotalPaid (Ether): 17, AvgGasPrice: 237GWei
	// Number: 10, BaseFee: 272GWei, TotalGasUsed: 80000000, TotalPaid (Ether): 19, AvgGasPrice: 241GWei
	// Number: 11, BaseFee: 281GWei, TotalGasUsed: 88000000, TotalPaid (Ether): 21, AvgGasPrice: 244GWei
	// Number: 12, BaseFee: 289GWei, TotalGasUsed: 96000000, TotalPaid (Ether): 23, AvgGasPrice: 248GWei
	// Number: 13, BaseFee: 298GWei, TotalGasUsed: 104000000, TotalPaid (Ether): 26, AvgGasPrice: 252GWei
	// Number: 14, BaseFee: 308GWei, TotalGasUsed: 112000000, TotalPaid (Ether): 28, AvgGasPrice: 256GWei
	// Number: 15, BaseFee: 318GWei, TotalGasUsed: 120000000, TotalPaid (Ether): 31, AvgGasPrice: 260GWei
	// Number: 16, BaseFee: 328GWei, TotalGasUsed: 128000000, TotalPaid (Ether): 33, AvgGasPrice: 264GWei
	// Number: 17, BaseFee: 338GWei, TotalGasUsed: 136000000, TotalPaid (Ether): 36, AvgGasPrice: 269GWei
	// Number: 18, BaseFee: 349GWei, TotalGasUsed: 144000000, TotalPaid (Ether): 39, AvgGasPrice: 273GWei
	// Number: 19, BaseFee: 360GWei, TotalGasUsed: 152000000, TotalPaid (Ether): 42, AvgGasPrice: 278GWei
	// ----- cortina ----
	// Number: 1, BaseFee: 225GWei, TotalGasUsed: 15000000, TotalPaid (Ether): 3, AvgGasPrice: 225GWei
	// Number: 2, BaseFee: 225GWei, TotalGasUsed: 30000000, TotalPaid (Ether): 6, AvgGasPrice: 225GWei
	// Number: 3, BaseFee: 231GWei, TotalGasUsed: 45000000, TotalPaid (Ether): 10, AvgGasPrice: 227GWei
	// Number: 4, BaseFee: 244GWei, TotalGasUsed: 60000000, TotalPaid (Ether): 13, AvgGasPrice: 231GWei
	// Number: 5, BaseFee: 264GWei, TotalGasUsed: 75000000, TotalPaid (Ether): 17, AvgGasPrice: 237GWei
	// Number: 6, BaseFee: 286GWei, TotalGasUsed: 90000000, TotalPaid (Ether): 22, AvgGasPrice: 246GWei
	// Number: 7, BaseFee: 310GWei, TotalGasUsed: 105000000, TotalPaid (Ether): 26, AvgGasPrice: 255GWei
	// Number: 8, BaseFee: 336GWei, TotalGasUsed: 120000000, TotalPaid (Ether): 31, AvgGasPrice: 265GWei
	// Number: 9, BaseFee: 364GWei, TotalGasUsed: 135000000, TotalPaid (Ether): 37, AvgGasPrice: 276GWei
	// Number: 10, BaseFee: 394GWei, TotalGasUsed: 150000000, TotalPaid (Ether): 43, AvgGasPrice: 288GWei
	// Number: 11, BaseFee: 427GWei, TotalGasUsed: 165000000, TotalPaid (Ether): 49, AvgGasPrice: 300GWei
	// Number: 12, BaseFee: 463GWei, TotalGasUsed: 180000000, TotalPaid (Ether): 56, AvgGasPrice: 314GWei
	// Number: 13, BaseFee: 501GWei, TotalGasUsed: 195000000, TotalPaid (Ether): 64, AvgGasPrice: 328GWei
	// Number: 14, BaseFee: 543GWei, TotalGasUsed: 210000000, TotalPaid (Ether): 72, AvgGasPrice: 344GWei
	// Number: 15, BaseFee: 588GWei, TotalGasUsed: 225000000, TotalPaid (Ether): 81, AvgGasPrice: 360GWei
	// Number: 16, BaseFee: 637GWei, TotalGasUsed: 240000000, TotalPaid (Ether): 90, AvgGasPrice: 377GWei
	// Number: 17, BaseFee: 690GWei, TotalGasUsed: 255000000, TotalPaid (Ether): 101, AvgGasPrice: 396GWei
	// Number: 18, BaseFee: 748GWei, TotalGasUsed: 270000000, TotalPaid (Ether): 112, AvgGasPrice: 415GWei
	// Number: 19, BaseFee: 810GWei, TotalGasUsed: 285000000, TotalPaid (Ether): 124, AvgGasPrice: 436GWei
}

func nextBlock(config *params.ChainConfig, parent *types.Header, gasUsed uint64) *types.Header {
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		Time:       parent.Time + 2,
	}
	if config.IsApricotPhase3(new(big.Int).SetUint64(header.Time)) {
		header.Extra, header.BaseFee, _ = dummy.CalcBaseFee(config, parent, header.Time)
	}
	header.GasUsed = gasUsed
	return header
}
