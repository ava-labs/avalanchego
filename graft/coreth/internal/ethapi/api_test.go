// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
// Copyright 2023 The go-ethereum Authors
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

package ethapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/graft/coreth/consensus"
	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/internal/blocktest"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/avalanchego/graft/evm/rpc"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/libevm/accounts"
	"github.com/ava-labs/libevm/accounts/keystore"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/bloombits"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/crypto/kzg4844"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/event"
	ethparams "github.com/ava-labs/libevm/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/slices"
)

func testTransactionMarshal(t *testing.T, tests []txData, config *params.ChainConfig) {
	t.Parallel()
	var (
		signer = types.LatestSigner(config)
		key, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	)

	for i, tt := range tests {
		var tx2 types.Transaction
		tx, err := types.SignNewTx(key, signer, tt.Tx)
		if err != nil {
			t.Fatalf("test %d: signing failed: %v", i, err)
		}
		// Regular transaction
		if data, err := json.Marshal(tx); err != nil {
			t.Fatalf("test %d: marshalling failed; %v", i, err)
		} else if err = tx2.UnmarshalJSON(data); err != nil {
			t.Fatalf("test %d: sunmarshal failed: %v", i, err)
		} else if want, have := tx.Hash(), tx2.Hash(); want != have {
			t.Fatalf("test %d: stx changed, want %x have %x", i, want, have)
		}

		// rpcTransaction
		rpcTx := newRPCTransaction(tx, common.Hash{}, 0, 0, 0, nil, config)
		if data, err := json.Marshal(rpcTx); err != nil {
			t.Fatalf("test %d: marshalling failed; %v", i, err)
		} else if err = tx2.UnmarshalJSON(data); err != nil {
			t.Fatalf("test %d: unmarshal failed: %v", i, err)
		} else if want, have := tx.Hash(), tx2.Hash(); want != have {
			t.Fatalf("test %d: tx changed, want %x have %x", i, want, have)
		} else {
			want, have := tt.Want, string(data)
			require.JSONEqf(t, want, have, "test %d: rpc json not match, want %s have %s", i, want, have)
		}
	}
}

func TestTransaction_RoundTripRpcJSON(t *testing.T) {
	var (
		config = params.TestChainConfig
		tests  = allTransactionTypes(common.Address{0xde, 0xad}, config)
	)
	testTransactionMarshal(t, tests, config)
}

func TestTransactionBlobTx(t *testing.T) {
	config := *params.TestChainConfig
	// config.ShanghaiTime = new(uint64)
	config.CancunTime = new(uint64)
	tests := allBlobTxs(common.Address{0xde, 0xad}, &config)

	testTransactionMarshal(t, tests, &config)
}

type txData struct {
	Tx   types.TxData
	Want string
}

func allTransactionTypes(addr common.Address, config *params.ChainConfig) []txData {
	return []txData{
		{
			Tx: &types.LegacyTx{
				Nonce:    5,
				GasPrice: big.NewInt(6),
				Gas:      7,
				To:       &addr,
				Value:    big.NewInt(8),
				Data:     []byte{0, 1, 2, 3, 4},
				V:        big.NewInt(9),
				R:        big.NewInt(10),
				S:        big.NewInt(11),
			},
			Want: `{
				"blockHash": null,
				"blockNumber": null,
				"from": "0x71562b71999873db5b286df957af199ec94617f7",
				"gas": "0x7",
				"gasPrice": "0x6",
				"hash": "0x3fa586d2448ae279279fa7036da74eb932763661543428c1a0aba21b95b37bdb",
				"input": "0x0001020304",
				"nonce": "0x5",
				"to": "0xdead000000000000000000000000000000000000",
				"transactionIndex": null,
				"value": "0x8",
				"type": "0x0",
				"chainId": "0x1",
				"v": "0x25",
				"r": "0xac639f4319e9268898e29444b97101f1225e2a0837151626da23e73dda2443fc",
				"s": "0x4fcc3f4c3a75f70ee45bb42d4b0aad432cc8c0140efb3e2611d6a6dda8460907"
			}`,
		}, {
			Tx: &types.LegacyTx{
				Nonce:    5,
				GasPrice: big.NewInt(6),
				Gas:      7,
				To:       nil,
				Value:    big.NewInt(8),
				Data:     []byte{0, 1, 2, 3, 4},
				V:        big.NewInt(32),
				R:        big.NewInt(10),
				S:        big.NewInt(11),
			},
			Want: `{
				"blockHash": null,
				"blockNumber": null,
				"from": "0x71562b71999873db5b286df957af199ec94617f7",
				"gas": "0x7",
				"gasPrice": "0x6",
				"hash": "0x617a316c6ff7ed2aa6ead1b4bb28a1322c2156c1c72f376a976d2d2adb1748ee",
				"input": "0x0001020304",
				"nonce": "0x5",
				"to": null,
				"transactionIndex": null,
				"value": "0x8",
				"type": "0x0",
				"chainId": "0x1",
				"v": "0x25",
				"r": "0xee8e89b513778d4815ae5969f3d55e0f7590f31b08f2a2290d5bc4ae37fce299",
				"s": "0x663db5c74c10e2b6525e7026e7cfd569b819ec91a042322655ff2b35060784b1"
			}`,
		},
		{
			Tx: &types.AccessListTx{
				ChainID:  config.ChainID,
				Nonce:    5,
				GasPrice: big.NewInt(6),
				Gas:      7,
				To:       &addr,
				Value:    big.NewInt(8),
				Data:     []byte{0, 1, 2, 3, 4},
				AccessList: types.AccessList{
					types.AccessTuple{
						Address:     common.Address{0x2},
						StorageKeys: []common.Hash{types.EmptyRootHash},
					},
				},
				V: big.NewInt(32),
				R: big.NewInt(10),
				S: big.NewInt(11),
			},
			Want: `{
				"blockHash": null,
				"blockNumber": null,
				"from": "0x71562b71999873db5b286df957af199ec94617f7",
				"gas": "0x7",
				"gasPrice": "0x6",
				"hash": "0x6becb7b9c171aa0d6d0a90dcd97bc3529c4d521f9cc9b7e31616aa9afc178c10",
				"input": "0x0001020304",
				"nonce": "0x5",
				"to": "0xdead000000000000000000000000000000000000",
				"transactionIndex": null,
				"value": "0x8",
				"type": "0x1",
				"accessList": [
					{
						"address": "0x0200000000000000000000000000000000000000",
						"storageKeys": [
							"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
						]
					}
				],
				"chainId": "0x1",
				"v": "0x1",
				"r": "0xea2289ca0243beecbe69d337bbc53c618e1fb6bd2ec69fd4121df47149a4d4be",
				"s": "0x2dc134b6bc43abbdfebef0b2d62c175459fc1e8ddff60c8e740c461d7ea1522f",
				"yParity": "0x1"
			}`,
		}, {
			Tx: &types.AccessListTx{
				ChainID:  config.ChainID,
				Nonce:    5,
				GasPrice: big.NewInt(6),
				Gas:      7,
				To:       nil,
				Value:    big.NewInt(8),
				Data:     []byte{0, 1, 2, 3, 4},
				AccessList: types.AccessList{
					types.AccessTuple{
						Address:     common.Address{0x2},
						StorageKeys: []common.Hash{types.EmptyRootHash},
					},
				},
				V: big.NewInt(32),
				R: big.NewInt(10),
				S: big.NewInt(11),
			},
			Want: `{
				"blockHash": null,
				"blockNumber": null,
				"from": "0x71562b71999873db5b286df957af199ec94617f7",
				"gas": "0x7",
				"gasPrice": "0x6",
				"hash": "0x22fbf81bae4640511c706e2c72d2f2ef1abc1e7861f2b82c4cae5b102a40709c",
				"input": "0x0001020304",
				"nonce": "0x5",
				"to": null,
				"transactionIndex": null,
				"value": "0x8",
				"type": "0x1",
				"accessList": [
					{
						"address": "0x0200000000000000000000000000000000000000",
						"storageKeys": [
							"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
						]
					}
				],
				"chainId": "0x1",
				"v": "0x1",
				"r": "0xc50e18edd861639735ec69ca12d82fcbb2c1921d2e2a8fd3a75f408d2d4b8118",
				"s": "0x32a908d1bc2db0229354f4dd392ffc37934e24ae8b18a620c6588c72660b6238",
				"yParity": "0x1"
			}`,
		}, {
			Tx: &types.DynamicFeeTx{
				ChainID:   config.ChainID,
				Nonce:     5,
				GasTipCap: big.NewInt(6),
				GasFeeCap: big.NewInt(9),
				Gas:       7,
				To:        &addr,
				Value:     big.NewInt(8),
				Data:      []byte{0, 1, 2, 3, 4},
				AccessList: types.AccessList{
					types.AccessTuple{
						Address:     common.Address{0x2},
						StorageKeys: []common.Hash{types.EmptyRootHash},
					},
				},
				V: big.NewInt(32),
				R: big.NewInt(10),
				S: big.NewInt(11),
			},
			Want: `{
				"blockHash": null,
				"blockNumber": null,
				"from": "0x71562b71999873db5b286df957af199ec94617f7",
				"gas": "0x7",
				"gasPrice": "0x9",
				"maxFeePerGas": "0x9",
				"maxPriorityFeePerGas": "0x6",
				"hash": "0xc5763d2ce6af3f694dcda8a9a50d4f75005a711edd382e993dd0406e0c54cfde",
				"input": "0x0001020304",
				"nonce": "0x5",
				"to": "0xdead000000000000000000000000000000000000",
				"transactionIndex": null,
				"value": "0x8",
				"type": "0x2",
				"accessList": [
					{
						"address": "0x0200000000000000000000000000000000000000",
						"storageKeys": [
							"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421"
						]
					}
				],
				"chainId": "0x1",
				"v": "0x0",
				"r": "0x740eb1e3bc206760182993845b7815fd8cf7a42f1a1ed26027f736e9eccfd20f",
				"s": "0x31da567e2b3a83e58e42f7902c3706926c926625f6978c24fdaa21b9d143bbf7",
				"yParity": "0x0"
			}`,
		}, {
			Tx: &types.DynamicFeeTx{
				ChainID:    config.ChainID,
				Nonce:      5,
				GasTipCap:  big.NewInt(6),
				GasFeeCap:  big.NewInt(9),
				Gas:        7,
				To:         nil,
				Value:      big.NewInt(8),
				Data:       []byte{0, 1, 2, 3, 4},
				AccessList: types.AccessList{},
				V:          big.NewInt(32),
				R:          big.NewInt(10),
				S:          big.NewInt(11),
			},
			Want: `{
				"blockHash": null,
				"blockNumber": null,
				"from": "0x71562b71999873db5b286df957af199ec94617f7",
				"gas": "0x7",
				"gasPrice": "0x9",
				"maxFeePerGas": "0x9",
				"maxPriorityFeePerGas": "0x6",
				"hash": "0x85545f69b2410640fbbb7157b9a79adece45bac4b2803733d250d049e9501a28",
				"input": "0x0001020304",
				"nonce": "0x5",
				"to": null,
				"transactionIndex": null,
				"value": "0x8",
				"type": "0x2",
				"accessList": [],
				"chainId": "0x1",
				"v": "0x1",
				"r": "0x5004538adbe499313737033b22eb2b50a9450f02fab3971a591e6d57761b2cdf",
				"s": "0x5f7b1f5d11bd467d84f32beb2e89629351b96c5204c4f72d5d2040bee369a73a",
				"yParity": "0x1"
			  }`,
		},
	}
}

func allBlobTxs(addr common.Address, config *params.ChainConfig) []txData {
	return []txData{
		{
			Tx: &types.BlobTx{
				Nonce:      6,
				GasTipCap:  uint256.NewInt(1),
				GasFeeCap:  uint256.NewInt(5),
				Gas:        6,
				To:         addr,
				BlobFeeCap: uint256.NewInt(1),
				BlobHashes: []common.Hash{{1}},
				Value:      new(uint256.Int),
				V:          uint256.NewInt(32),
				R:          uint256.NewInt(10),
				S:          uint256.NewInt(11),
			},
			Want: `{
                "blockHash": null,
                "blockNumber": null,
                "from": "0x71562b71999873db5b286df957af199ec94617f7",
                "gas": "0x6",
                "gasPrice": "0x5",
                "maxFeePerGas": "0x5",
                "maxPriorityFeePerGas": "0x1",
                "maxFeePerBlobGas": "0x1",
                "hash": "0x1f2b59a20e61efc615ad0cbe936379d6bbea6f938aafaf35eb1da05d8e7f46a3",
                "input": "0x",
                "nonce": "0x6",
                "to": "0xdead000000000000000000000000000000000000",
                "transactionIndex": null,
                "value": "0x0",
                "type": "0x3",
                "accessList": [],
                "chainId": "0x1",
                "blobVersionedHashes": [
                    "0x0100000000000000000000000000000000000000000000000000000000000000"
                ],
                "v": "0x0",
                "r": "0x618be8908e0e5320f8f3b48042a079fe5a335ebd4ed1422a7d2207cd45d872bc",
                "s": "0x27b2bc6c80e849a8e8b764d4549d8c2efac3441e73cf37054eb0a9b9f8e89b27",
                "yParity": "0x0"
            }`,
		},
	}
}

func newTestAccountManager(t *testing.T) (*accounts.Manager, accounts.Account) {
	var (
		dir        = t.TempDir()
		am         = accounts.NewManager(&accounts.Config{InsecureUnlockAllowed: true})
		b          = keystore.NewKeyStore(dir, 2, 1)
		testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	)
	acc, err := b.ImportECDSA(testKey, "")
	if err != nil {
		t.Fatalf("failed to create test account: %v", err)
	}
	if err := b.Unlock(acc, ""); err != nil {
		t.Fatalf("failed to unlock account: %v\n", err)
	}
	am.AddBackend(b)
	return am, acc
}

type testBackend struct {
	db     ethdb.Database
	chain  *core.BlockChain
	accman *accounts.Manager
	acc    accounts.Account
}

func newTestBackend(t *testing.T, n int, gspec *core.Genesis, engine consensus.Engine, generator func(i int, b *core.BlockGen)) *testBackend {
	var (
		cacheConfig = &core.CacheConfig{
			TrieCleanLimit: 256,
			TrieDirtyLimit: 256,
			SnapshotLimit:  0,
			Pruning:        false, // Archive mode
		}
	)
	accman, acc := newTestAccountManager(t)
	gspec.Alloc[acc.Address] = types.Account{Balance: big.NewInt(params.Ether)}
	// Generate blocks for testing
	db, blocks, _, _ := core.GenerateChainWithGenesis(gspec, engine, n, 10, generator)
	chain, err := core.NewBlockChain(db, cacheConfig, gspec, engine, vm.Config{}, gspec.ToBlock().Hash(), false)
	if err != nil {
		t.Fatalf("failed to create tester chain: %v", err)
	}
	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("block %d: failed to insert into chain: %v", n, err)
	}
	for _, block := range blocks {
		if err := chain.Accept(block); err != nil {
			t.Fatalf("block %d: failed to accept into chain: %v", block.NumberU64(), err)
		}
	}
	chain.DrainAcceptorQueue()

	backend := &testBackend{db: db, chain: chain, accman: accman, acc: acc}
	return backend
}

func (b testBackend) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (b testBackend) FeeHistory(ctx context.Context, blockCount uint64, lastBlock rpc.BlockNumber, rewardPercentiles []float64) (*big.Int, [][]*big.Int, []*big.Int, []float64, error) {
	return nil, nil, nil, nil, nil
}
func (b testBackend) ChainDb() ethdb.Database                    { return b.db }
func (b testBackend) AccountManager() *accounts.Manager          { return b.accman }
func (b testBackend) ExtRPCEnabled() bool                        { return false }
func (b testBackend) RPCGasCap() uint64                          { return 10000000 }
func (b testBackend) RPCEVMTimeout() time.Duration               { return time.Second }
func (b testBackend) RPCTxFeeCap() float64                       { return 0 }
func (b testBackend) PriceOptionsConfig() PriceOptionConfig      { return PriceOptionConfig{} }
func (b testBackend) UnprotectedAllowed(*types.Transaction) bool { return false }
func (b testBackend) SetHead(number uint64)                      {}
func (b testBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	if number == rpc.LatestBlockNumber {
		return b.chain.CurrentBlock(), nil
	}
	return b.chain.GetHeaderByNumber(uint64(number)), nil
}
func (b testBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return b.chain.GetHeaderByHash(hash), nil
}
func (b testBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.HeaderByNumber(ctx, blockNr)
	}
	if blockHash, ok := blockNrOrHash.Hash(); ok {
		return b.HeaderByHash(ctx, blockHash)
	}
	panic("unknown type rpc.BlockNumberOrHash")
}
func (b testBackend) CurrentHeader() *types.Header { return b.chain.CurrentBlock() }
func (b testBackend) CurrentBlock() *types.Header  { return b.chain.CurrentBlock() }
func (b testBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	if number == rpc.LatestBlockNumber {
		head := b.chain.CurrentBlock()
		return b.chain.GetBlock(head.Hash(), head.Number.Uint64()), nil
	}
	return b.chain.GetBlockByNumber(uint64(number)), nil
}
func (b testBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return b.chain.GetBlockByHash(hash), nil
}
func (b testBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.BlockByNumber(ctx, blockNr)
	}
	if blockHash, ok := blockNrOrHash.Hash(); ok {
		return b.BlockByHash(ctx, blockHash)
	}
	panic("unknown type rpc.BlockNumberOrHash")
}
func (b testBackend) GetBody(ctx context.Context, hash common.Hash, number rpc.BlockNumber) (*types.Body, error) {
	return b.chain.GetBlock(hash, uint64(number.Int64())).Body(), nil
}
func (b testBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*state.StateDB, *types.Header, error) {
	if number == rpc.PendingBlockNumber {
		panic("pending state not implemented")
	}
	header, err := b.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, nil, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	stateDb, err := b.chain.StateAt(header.Root)
	return stateDb, header, err
}
func (b testBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*state.StateDB, *types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.StateAndHeaderByNumber(ctx, blockNr)
	}
	panic("only implemented for number")
}
func (b testBackend) PendingBlockAndReceipts() (*types.Block, types.Receipts) { panic("implement me") }
func (b testBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	header, err := b.HeaderByHash(ctx, hash)
	if header == nil || err != nil {
		return nil, err
	}
	receipts := rawdb.ReadReceipts(b.db, hash, header.Number.Uint64(), header.Time, b.chain.Config())
	return receipts, nil
}
func (b testBackend) GetEVM(ctx context.Context, msg *core.Message, state *state.StateDB, header *types.Header, vmConfig *vm.Config, blockContext *vm.BlockContext) *vm.EVM {
	if vmConfig == nil {
		vmConfig = b.chain.GetVMConfig()
	}
	txContext := core.NewEVMTxContext(msg)
	context := core.NewEVMBlockContext(header, b.chain, nil)
	if blockContext != nil {
		context = *blockContext
	}
	return vm.NewEVM(context, txContext, state, b.chain.Config(), *vmConfig)
}
func (b testBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	panic("implement me")
}
func (b testBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	panic("implement me")
}
func (b testBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	panic("implement me")
}
func (b testBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	panic("implement me")
}
func (b testBackend) GetTransaction(ctx context.Context, txHash common.Hash) (bool, *types.Transaction, common.Hash, uint64, uint64, error) {
	tx, blockHash, blockNumber, index := rawdb.ReadTransaction(b.db, txHash)
	return true, tx, blockHash, blockNumber, index, nil
}
func (b testBackend) GetPoolTransactions() (types.Transactions, error)         { panic("implement me") }
func (b testBackend) GetPoolTransaction(txHash common.Hash) *types.Transaction { panic("implement me") }
func (b testBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return 0, nil
}
func (b testBackend) Stats() (pending int, queued int) { panic("implement me") }
func (b testBackend) TxPoolContent() (map[common.Address][]*types.Transaction, map[common.Address][]*types.Transaction) {
	panic("implement me")
}
func (b testBackend) TxPoolContentFrom(addr common.Address) ([]*types.Transaction, []*types.Transaction) {
	panic("implement me")
}
func (b testBackend) SubscribeNewTxsEvent(events chan<- core.NewTxsEvent) event.Subscription {
	panic("implement me")
}
func (b testBackend) ChainConfig() *params.ChainConfig { return b.chain.Config() }
func (b testBackend) Engine() consensus.Engine         { return b.chain.Engine() }
func (b testBackend) GetLogs(ctx context.Context, blockHash common.Hash, number uint64) ([][]*types.Log, error) {
	panic("implement me")
}
func (b testBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	panic("implement me")
}
func (b testBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	panic("implement me")
}
func (b testBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	panic("implement me")
}
func (b testBackend) BloomStatus() (uint64, uint64) { panic("implement me") }
func (b testBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	panic("implement me")
}
func (b testBackend) BadBlocks() ([]*types.Block, []*core.BadBlockReason) { return nil, nil }
func (b testBackend) EstimateBaseFee(ctx context.Context) (*big.Int, error) {
	panic("implement me")
}
func (b testBackend) LastAcceptedBlock() *types.Block { panic("implement me") }
func (b testBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	panic("implement me")
}
func (b testBackend) IsArchive() bool {
	panic("implement me")
}
func (b testBackend) HistoricalProofQueryWindow() (queryWindow uint64) {
	panic("implement me")
}

func TestEstimateGas(t *testing.T) {
	t.Parallel()
	// Initialize test accounts
	var (
		accounts = newAccounts(2)
		genesis  = &core.Genesis{
			Config: params.TestChainConfig,
			Alloc: types.GenesisAlloc{
				accounts[0].addr: {Balance: big.NewInt(params.Ether)},
				accounts[1].addr: {Balance: big.NewInt(params.Ether)},
			},
		}
		genBlocks      = 10
		signer         = types.HomesteadSigner{}
		randomAccounts = newAccounts(2)
	)
	api := NewBlockChainAPI(newTestBackend(t, genBlocks, genesis, dummy.NewCoinbaseFaker(), func(i int, b *core.BlockGen) {
		// Transfer from account[0] to account[1]
		//    value: 1000 wei
		//    fee:   0 wei
		tx, _ := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(i), To: &accounts[1].addr, Value: big.NewInt(1000), Gas: ethparams.TxGas, GasPrice: b.BaseFee(), Data: nil}), signer, accounts[0].key)
		b.AddTx(tx)
		// b.SetPoS()
	}))
	var testSuite = []struct {
		blockNumber rpc.BlockNumber
		call        TransactionArgs
		overrides   StateOverride
		expectErr   error
		want        uint64
	}{
		// simple transfer on latest block
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:  &accounts[0].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			expectErr: nil,
			want:      21000,
		},
		// simple transfer with insufficient funds on latest block
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:  &randomAccounts[0].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			expectErr: core.ErrInsufficientFunds,
			want:      21000,
		},
		// empty create
		{
			blockNumber: rpc.LatestBlockNumber,
			call:        TransactionArgs{},
			expectErr:   nil,
			want:        53000,
		},
		{
			blockNumber: rpc.LatestBlockNumber,
			call:        TransactionArgs{},
			overrides: StateOverride{
				randomAccounts[0].addr: OverrideAccount{Balance: newRPCBalance(new(big.Int).Mul(big.NewInt(1), big.NewInt(params.Ether)))},
			},
			expectErr: nil,
			want:      53000,
		},
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:  &randomAccounts[0].addr,
				To:    &randomAccounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			overrides: StateOverride{
				randomAccounts[0].addr: OverrideAccount{Balance: newRPCBalance(big.NewInt(0))},
			},
			expectErr: core.ErrInsufficientFunds,
		},
		// Test for a bug where the gas price was set to zero but the basefee non-zero
		//
		// contract BasefeeChecker {
		//    constructor() {
		//        require(tx.gasprice >= block.basefee);
		//        if (tx.gasprice > 0) {
		//            require(block.basefee > 0);
		//        }
		//    }
		//}
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:     &accounts[0].addr,
				Input:    hex2Bytes("6080604052348015600f57600080fd5b50483a1015601c57600080fd5b60003a111560315760004811603057600080fd5b5b603f80603e6000396000f3fe6080604052600080fdfea264697066735822122060729c2cee02b10748fae5200f1c9da4661963354973d9154c13a8e9ce9dee1564736f6c63430008130033"),
				GasPrice: (*hexutil.Big)(big.NewInt(ap3.InitialBaseFee)), // Legacy as pricing
			},
			expectErr: nil,
			want:      67617,
		},
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:         &accounts[0].addr,
				Input:        hex2Bytes("6080604052348015600f57600080fd5b50483a1015601c57600080fd5b60003a111560315760004811603057600080fd5b5b603f80603e6000396000f3fe6080604052600080fdfea264697066735822122060729c2cee02b10748fae5200f1c9da4661963354973d9154c13a8e9ce9dee1564736f6c63430008130033"),
				MaxFeePerGas: (*hexutil.Big)(big.NewInt(ap3.InitialBaseFee)), // 1559 gas pricing
			},
			expectErr: nil,
			want:      67617,
		},
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:         &accounts[0].addr,
				Input:        hex2Bytes("6080604052348015600f57600080fd5b50483a1015601c57600080fd5b60003a111560315760004811603057600080fd5b5b603f80603e6000396000f3fe6080604052600080fdfea264697066735822122060729c2cee02b10748fae5200f1c9da4661963354973d9154c13a8e9ce9dee1564736f6c63430008130033"),
				GasPrice:     nil, // No legacy gas pricing
				MaxFeePerGas: nil, // No 1559 gas pricing
			},
			expectErr: nil,
			want:      67595,
		},
		// Blobs should have no effect on gas estimate
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:       &accounts[0].addr,
				To:         &accounts[1].addr,
				Value:      (*hexutil.Big)(big.NewInt(1)),
				BlobHashes: []common.Hash{common.Hash{0x01, 0x22}},
				BlobFeeCap: (*hexutil.Big)(big.NewInt(1)),
			},
			want: 21000,
		},
	}
	for i, tc := range testSuite {
		result, err := api.EstimateGas(context.Background(), tc.call, &rpc.BlockNumberOrHash{BlockNumber: &tc.blockNumber}, &tc.overrides)
		if tc.expectErr != nil {
			if err == nil {
				t.Errorf("test %d: want error %v, have nothing", i, tc.expectErr)
				continue
			}
			if !errors.Is(err, tc.expectErr) {
				t.Errorf("test %d: error mismatch, want %v, have %v", i, tc.expectErr, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("test %d: want no error, have %v", i, err)
			continue
		}
		if float64(result) > float64(tc.want)*(1+estimateGasErrorRatio) {
			t.Errorf("test %d, result mismatch, have\n%v\n, want\n%v\n", i, uint64(result), tc.want)
		}
	}
}

func TestCall(t *testing.T) {
	// Enable BLOBHASH opcode in Cancun
	cfg := *params.TestChainConfig
	cfg.ShanghaiTime = utils.NewUint64(0)
	cfg.CancunTime = utils.NewUint64(0)
	t.Parallel()
	// Initialize test accounts
	var (
		accounts = newAccounts(3)
		genesis  = &core.Genesis{
			Config: &cfg,
			Alloc: types.GenesisAlloc{
				accounts[0].addr: {Balance: big.NewInt(params.Ether)},
				accounts[1].addr: {Balance: big.NewInt(params.Ether)},
				accounts[2].addr: {Balance: big.NewInt(params.Ether)},
			},
		}
		genBlocks = 10
		signer    = types.HomesteadSigner{}
	)
	api := NewBlockChainAPI(newTestBackend(t, genBlocks, genesis, dummy.NewCoinbaseFaker(), func(i int, b *core.BlockGen) {
		// Transfer from account[0] to account[1]
		//    value: 1000 wei
		//    fee:   0 wei
		tx, _ := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(i), To: &accounts[1].addr, Value: big.NewInt(1000), Gas: ethparams.TxGas, GasPrice: b.BaseFee(), Data: nil}), signer, accounts[0].key)
		b.AddTx(tx)
		// b.SetPoS()
	}))
	randomAccounts := newAccounts(3)
	var testSuite = []struct {
		blockNumber    rpc.BlockNumber
		overrides      StateOverride
		call           TransactionArgs
		blockOverrides BlockOverrides
		expectErr      error
		want           string
	}{
		// transfer on genesis
		{
			blockNumber: rpc.BlockNumber(0),
			call: TransactionArgs{
				From:  &accounts[0].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			expectErr: nil,
			want:      "0x",
		},
		// transfer on the head
		{
			blockNumber: rpc.BlockNumber(genBlocks),
			call: TransactionArgs{
				From:  &accounts[0].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			expectErr: nil,
			want:      "0x",
		},
		// transfer on a non-existent block, error expects
		{
			blockNumber: rpc.BlockNumber(genBlocks + 1),
			call: TransactionArgs{
				From:  &accounts[0].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			expectErr: errors.New("header not found"),
		},
		// transfer on the latest block
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:  &accounts[0].addr,
				To:    &accounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			expectErr: nil,
			want:      "0x",
		},
		// Call which can only succeed if state is state overridden
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:  &randomAccounts[0].addr,
				To:    &randomAccounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			overrides: StateOverride{
				randomAccounts[0].addr: OverrideAccount{Balance: newRPCBalance(new(big.Int).Mul(big.NewInt(1), big.NewInt(params.Ether)))},
			},
			want: "0x",
		},
		// Invalid call without state overriding
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:  &randomAccounts[0].addr,
				To:    &randomAccounts[1].addr,
				Value: (*hexutil.Big)(big.NewInt(1000)),
			},
			expectErr: core.ErrInsufficientFunds,
		},
		// Successful simple contract call
		//
		// // SPDX-License-Identifier: GPL-3.0
		//
		//  pragma solidity >=0.7.0 <0.8.0;
		//
		//  /**
		//   * @title Storage
		//   * @dev Store & retrieve value in a variable
		//   */
		//  contract Storage {
		//      uint256 public number;
		//      constructor() {
		//          number = block.number;
		//      }
		//  }
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From: &randomAccounts[0].addr,
				To:   &randomAccounts[2].addr,
				Data: hex2Bytes("8381f58a"), // call number()
			},
			overrides: StateOverride{
				randomAccounts[2].addr: OverrideAccount{
					Code:      hex2Bytes("6080604052348015600f57600080fd5b506004361060285760003560e01c80638381f58a14602d575b600080fd5b60336049565b6040518082815260200191505060405180910390f35b6000548156fea2646970667358221220eab35ffa6ab2adfe380772a48b8ba78e82a1b820a18fcb6f59aa4efb20a5f60064736f6c63430007040033"),
					StateDiff: &map[common.Hash]common.Hash{{}: common.BigToHash(big.NewInt(123))},
				},
			},
			want: "0x000000000000000000000000000000000000000000000000000000000000007b",
		},
		// Block overrides should work
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From: &accounts[1].addr,
				Input: &hexutil.Bytes{
					0x43,             // NUMBER
					0x60, 0x00, 0x52, // MSTORE offset 0
					0x60, 0x20, 0x60, 0x00, 0xf3,
				},
			},
			blockOverrides: BlockOverrides{Number: (*hexutil.Big)(big.NewInt(11))},
			want:           "0x000000000000000000000000000000000000000000000000000000000000000b",
		},
		// Invalid blob tx
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:       &accounts[1].addr,
				Input:      &hexutil.Bytes{0x00},
				BlobHashes: []common.Hash{},
			},
			expectErr: core.ErrBlobTxCreate,
		},
		// BLOBHASH opcode
		{
			blockNumber: rpc.LatestBlockNumber,
			call: TransactionArgs{
				From:       &accounts[1].addr,
				To:         &randomAccounts[2].addr,
				BlobHashes: []common.Hash{common.Hash{0x01, 0x22}},
				BlobFeeCap: (*hexutil.Big)(big.NewInt(1)),
			},
			overrides: StateOverride{
				randomAccounts[2].addr: {
					Code: hex2Bytes("60004960005260206000f3"),
				},
			},
			want: "0x0122000000000000000000000000000000000000000000000000000000000000",
		},
	}
	for i, tc := range testSuite {
		result, err := api.Call(context.Background(), tc.call, &rpc.BlockNumberOrHash{BlockNumber: &tc.blockNumber}, &tc.overrides, &tc.blockOverrides)
		if tc.expectErr != nil {
			if err == nil {
				t.Errorf("test %d: want error %v, have nothing", i, tc.expectErr)
				continue
			}
			if !errors.Is(err, tc.expectErr) {
				// Second try
				if !reflect.DeepEqual(err, tc.expectErr) {
					t.Errorf("test %d: error mismatch, want %v, have %v", i, tc.expectErr, err)
				}
			}
			continue
		}
		if err != nil {
			t.Errorf("test %d: want no error, have %v", i, err)
			continue
		}
		if !reflect.DeepEqual(result.String(), tc.want) {
			t.Errorf("test %d, result mismatch, have\n%v\n, want\n%v\n", i, result.String(), tc.want)
		}
	}
}

func TestSignTransaction(t *testing.T) {
	t.Parallel()
	// Initialize test accounts
	var (
		key, _  = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		to      = crypto.PubkeyToAddress(key.PublicKey)
		genesis = &core.Genesis{
			Config: params.TestChainConfig,
			Alloc:  types.GenesisAlloc{},
		}
	)
	b := newTestBackend(t, 1, genesis, dummy.NewCoinbaseFaker(), func(i int, b *core.BlockGen) {
		// b.SetPoS()
	})
	api := NewTransactionAPI(b, nil)
	res, err := api.FillTransaction(context.Background(), TransactionArgs{
		From:  &b.acc.Address,
		To:    &to,
		Value: (*hexutil.Big)(big.NewInt(1)),
	})
	if err != nil {
		t.Fatalf("failed to fill tx defaults: %v\n", err)
	}

	res, err = api.SignTransaction(context.Background(), argsFromTransaction(res.Tx, b.acc.Address))
	if err != nil {
		t.Fatalf("failed to sign tx: %v\n", err)
	}
	tx, err := json.Marshal(res.Tx)
	if err != nil {
		t.Fatal(err)
	}
	// The expected result has deviated from upstream because the base fee, and
	// therefore the `maxFeePerGas`, resulting from [params.TestChainConfig] is
	// different.
	expect := `{"type":"0x2","chainId":"0x1","nonce":"0x0","to":"0x703c4b2bd70c169f5717101caee543299fc946c7","gas":"0x5208","gasPrice":null,"maxPriorityFeePerGas":"0x0","maxFeePerGas":"0x2","value":"0x1","input":"0x","accessList":[],"v":"0x1","r":"0x5a32230e497be0277b58afb995227a167e087462fb770057ed6946f5ef5a2df5","s":"0x431e048124baffbd67bc35df940bb9f5ddf8a36afb2672616d075ac39415e885","yParity":"0x1","hash":"0xf5e941beeca516d3d3dca2707d74c54a58e07365b89efc5de58dd7b6041ef78e"}`
	if !bytes.Equal(tx, []byte(expect)) {
		t.Errorf("result mismatch. Have:\n%s\nWant:\n%s\n", tx, expect)
	}
}

func TestSignBlobTransaction(t *testing.T) {
	t.Parallel()
	// Initialize test accounts
	var (
		key, _  = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		to      = crypto.PubkeyToAddress(key.PublicKey)
		genesis = &core.Genesis{
			Config: params.TestChainConfig,
			Alloc:  types.GenesisAlloc{},
		}
	)
	b := newTestBackend(t, 1, genesis, dummy.NewCoinbaseFaker(), func(i int, b *core.BlockGen) {
		// b.SetPoS()
	})
	api := NewTransactionAPI(b, nil)
	res, err := api.FillTransaction(context.Background(), TransactionArgs{
		From:       &b.acc.Address,
		To:         &to,
		Value:      (*hexutil.Big)(big.NewInt(1)),
		BlobHashes: []common.Hash{{0x01, 0x22}},
	})
	if err != nil {
		t.Fatalf("failed to fill tx defaults: %v\n", err)
	}

	_, err = api.SignTransaction(context.Background(), argsFromTransaction(res.Tx, b.acc.Address))
	if err == nil {
		t.Fatalf("should fail on blob transaction")
	}
	if !errors.Is(err, errBlobTxNotSupported) {
		t.Errorf("error mismatch. Have: %v, want: %v", err, errBlobTxNotSupported)
	}
}

func TestSendBlobTransaction(t *testing.T) {
	t.Parallel()
	// Initialize test accounts
	var (
		key, _  = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		to      = crypto.PubkeyToAddress(key.PublicKey)
		genesis = &core.Genesis{
			Config: params.TestChainConfig,
			Alloc:  types.GenesisAlloc{},
		}
	)
	b := newTestBackend(t, 1, genesis, dummy.NewCoinbaseFaker(), func(i int, b *core.BlockGen) {
		// b.SetPoS()
	})
	api := NewTransactionAPI(b, nil)
	res, err := api.FillTransaction(context.Background(), TransactionArgs{
		From:       &b.acc.Address,
		To:         &to,
		Value:      (*hexutil.Big)(big.NewInt(1)),
		BlobHashes: []common.Hash{common.Hash{0x01, 0x22}},
	})
	if err != nil {
		t.Fatalf("failed to fill tx defaults: %v\n", err)
	}

	_, err = api.SendTransaction(context.Background(), argsFromTransaction(res.Tx, b.acc.Address))
	if err == nil {
		t.Errorf("sending tx should have failed")
	} else if !errors.Is(err, errBlobTxNotSupported) {
		t.Errorf("unexpected error. Have %v, want %v\n", err, errBlobTxNotSupported)
	}
}

func TestFillBlobTransaction(t *testing.T) {
	t.Parallel()
	// Initialize test accounts
	var (
		key, _  = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		to      = crypto.PubkeyToAddress(key.PublicKey)
		genesis = &core.Genesis{
			Config: params.TestChainConfig,
			Alloc:  types.GenesisAlloc{},
		}
		emptyBlob                      = new(kzg4844.Blob)
		emptyBlobs                     = []kzg4844.Blob{*emptyBlob}
		emptyBlobCommit, _             = kzg4844.BlobToCommitment(emptyBlob)
		emptyBlobProof, _              = kzg4844.ComputeBlobProof(emptyBlob, emptyBlobCommit)
		emptyBlobHash      common.Hash = kzg4844.CalcBlobHashV1(sha256.New(), &emptyBlobCommit)
	)
	b := newTestBackend(t, 1, genesis, dummy.NewCoinbaseFaker(), func(i int, b *core.BlockGen) {
		// b.SetPoS()
	})
	api := NewTransactionAPI(b, nil)
	type result struct {
		Hashes  []common.Hash
		Sidecar *types.BlobTxSidecar
	}
	suite := []struct {
		name string
		args TransactionArgs
		err  string
		want *result
	}{
		{
			name: "TestInvalidParamsCombination1",
			args: TransactionArgs{
				From:   &b.acc.Address,
				To:     &to,
				Value:  (*hexutil.Big)(big.NewInt(1)),
				Blobs:  []kzg4844.Blob{{}},
				Proofs: []kzg4844.Proof{{}},
			},
			err: `blob proofs provided while commitments were not`,
		},
		{
			name: "TestInvalidParamsCombination2",
			args: TransactionArgs{
				From:        &b.acc.Address,
				To:          &to,
				Value:       (*hexutil.Big)(big.NewInt(1)),
				Blobs:       []kzg4844.Blob{{}},
				Commitments: []kzg4844.Commitment{{}},
			},
			err: `blob commitments provided while proofs were not`,
		},
		{
			name: "TestInvalidParamsCount1",
			args: TransactionArgs{
				From:        &b.acc.Address,
				To:          &to,
				Value:       (*hexutil.Big)(big.NewInt(1)),
				Blobs:       []kzg4844.Blob{{}},
				Commitments: []kzg4844.Commitment{{}, {}},
				Proofs:      []kzg4844.Proof{{}, {}},
			},
			err: `number of blobs and commitments mismatch (have=2, want=1)`,
		},
		{
			name: "TestInvalidParamsCount2",
			args: TransactionArgs{
				From:        &b.acc.Address,
				To:          &to,
				Value:       (*hexutil.Big)(big.NewInt(1)),
				Blobs:       []kzg4844.Blob{{}, {}},
				Commitments: []kzg4844.Commitment{{}, {}},
				Proofs:      []kzg4844.Proof{{}},
			},
			err: `number of blobs and proofs mismatch (have=1, want=2)`,
		},
		{
			name: "TestInvalidProofVerification",
			args: TransactionArgs{
				From:        &b.acc.Address,
				To:          &to,
				Value:       (*hexutil.Big)(big.NewInt(1)),
				Blobs:       []kzg4844.Blob{{}, {}},
				Commitments: []kzg4844.Commitment{{}, {}},
				Proofs:      []kzg4844.Proof{{}, {}},
			},
			err: `failed to verify blob proof: short buffer`,
		},
		{
			name: "TestGenerateBlobHashes",
			args: TransactionArgs{
				From:        &b.acc.Address,
				To:          &to,
				Value:       (*hexutil.Big)(big.NewInt(1)),
				Blobs:       emptyBlobs,
				Commitments: []kzg4844.Commitment{emptyBlobCommit},
				Proofs:      []kzg4844.Proof{emptyBlobProof},
			},
			want: &result{
				Hashes: []common.Hash{emptyBlobHash},
				Sidecar: &types.BlobTxSidecar{
					Blobs:       emptyBlobs,
					Commitments: []kzg4844.Commitment{emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof},
				},
			},
		},
		{
			name: "TestValidBlobHashes",
			args: TransactionArgs{
				From:        &b.acc.Address,
				To:          &to,
				Value:       (*hexutil.Big)(big.NewInt(1)),
				BlobHashes:  []common.Hash{emptyBlobHash},
				Blobs:       emptyBlobs,
				Commitments: []kzg4844.Commitment{emptyBlobCommit},
				Proofs:      []kzg4844.Proof{emptyBlobProof},
			},
			want: &result{
				Hashes: []common.Hash{emptyBlobHash},
				Sidecar: &types.BlobTxSidecar{
					Blobs:       emptyBlobs,
					Commitments: []kzg4844.Commitment{emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof},
				},
			},
		},
		{
			name: "TestInvalidBlobHashes",
			args: TransactionArgs{
				From:        &b.acc.Address,
				To:          &to,
				Value:       (*hexutil.Big)(big.NewInt(1)),
				BlobHashes:  []common.Hash{{0x01, 0x22}},
				Blobs:       emptyBlobs,
				Commitments: []kzg4844.Commitment{emptyBlobCommit},
				Proofs:      []kzg4844.Proof{emptyBlobProof},
			},
			err: fmt.Sprintf("blob hash verification failed (have=%s, want=%s)", common.Hash{0x01, 0x22}, emptyBlobHash),
		},
		{
			name: "TestGenerateBlobProofs",
			args: TransactionArgs{
				From:  &b.acc.Address,
				To:    &to,
				Value: (*hexutil.Big)(big.NewInt(1)),
				Blobs: emptyBlobs,
			},
			want: &result{
				Hashes: []common.Hash{emptyBlobHash},
				Sidecar: &types.BlobTxSidecar{
					Blobs:       emptyBlobs,
					Commitments: []kzg4844.Commitment{emptyBlobCommit},
					Proofs:      []kzg4844.Proof{emptyBlobProof},
				},
			},
		},
	}
	for _, tc := range suite {
		t.Run(tc.name, func(t *testing.T) {
			res, err := api.FillTransaction(context.Background(), tc.args)
			if len(tc.err) > 0 {
				if err == nil {
					t.Fatalf("missing error. want: %s", tc.err)
				} else if err != nil && err.Error() != tc.err {
					t.Fatalf("error mismatch. want: %s, have: %s", tc.err, err.Error())
				}
				return
			}
			if err != nil && len(tc.err) == 0 {
				t.Fatalf("expected no error. have: %s", err)
			}
			if res == nil {
				t.Fatal("result missing")
			}
			want, err := json.Marshal(tc.want)
			if err != nil {
				t.Fatalf("failed to encode expected: %v", err)
			}
			have, err := json.Marshal(result{Hashes: res.Tx.BlobHashes(), Sidecar: res.Tx.BlobTxSidecar()})
			if err != nil {
				t.Fatalf("failed to encode computed sidecar: %v", err)
			}
			if !bytes.Equal(have, want) {
				t.Errorf("blob sidecar mismatch. Have: %s, want: %s", have, want)
			}
		})
	}
}

func argsFromTransaction(tx *types.Transaction, from common.Address) TransactionArgs {
	var (
		gas   = tx.Gas()
		nonce = tx.Nonce()
		input = tx.Data()
	)
	return TransactionArgs{
		From:                 &from,
		To:                   tx.To(),
		Gas:                  (*hexutil.Uint64)(&gas),
		MaxFeePerGas:         (*hexutil.Big)(tx.GasFeeCap()),
		MaxPriorityFeePerGas: (*hexutil.Big)(tx.GasTipCap()),
		Value:                (*hexutil.Big)(tx.Value()),
		Nonce:                (*hexutil.Uint64)(&nonce),
		Input:                (*hexutil.Bytes)(&input),
		ChainID:              (*hexutil.Big)(tx.ChainId()),
		// TODO: impl accessList conversion
		//AccessList: tx.AccessList(),
		BlobFeeCap: (*hexutil.Big)(tx.BlobGasFeeCap()),
		BlobHashes: tx.BlobHashes(),
	}
}

type account struct {
	key  *ecdsa.PrivateKey
	addr common.Address
}

func newAccounts(n int) (accounts []account) {
	for i := 0; i < n; i++ {
		key, _ := crypto.GenerateKey()
		addr := crypto.PubkeyToAddress(key.PublicKey)
		accounts = append(accounts, account{key: key, addr: addr})
	}
	slices.SortFunc(accounts, func(a, b account) int { return a.addr.Cmp(b.addr) })
	return accounts
}

func newRPCBalance(balance *big.Int) **hexutil.Big {
	rpcBalance := (*hexutil.Big)(balance)
	return &rpcBalance
}

func hex2Bytes(str string) *hexutil.Bytes {
	rpcBytes := hexutil.Bytes(common.Hex2Bytes(str))
	return &rpcBytes
}

func TestRPCMarshalBlock(t *testing.T) {
	t.Parallel()
	var (
		txs []*types.Transaction
		to  = common.BytesToAddress([]byte{0x11})
	)
	for i := uint64(1); i <= 4; i++ {
		var tx *types.Transaction
		if i%2 == 0 {
			tx = types.NewTx(&types.LegacyTx{
				Nonce:    i,
				GasPrice: big.NewInt(11111),
				Gas:      1111,
				To:       &to,
				Value:    big.NewInt(111),
				Data:     []byte{0x11, 0x11, 0x11},
			})
		} else {
			tx = types.NewTx(&types.AccessListTx{
				ChainID:  big.NewInt(1337),
				Nonce:    i,
				GasPrice: big.NewInt(11111),
				Gas:      1111,
				To:       &to,
				Value:    big.NewInt(111),
				Data:     []byte{0x11, 0x11, 0x11},
			})
		}
		txs = append(txs, tx)
	}
	block := types.NewBlock(&types.Header{Number: big.NewInt(100)}, txs, nil, nil, blocktest.NewHasher())

	var testSuite = []struct {
		inclTx bool
		fullTx bool
		want   string
	}{
		// without txs
		{
			inclTx: false,
			fullTx: false,
			want: `{
				"blockExtraData":"0x",
				"difficulty": "0x0",
				"extDataHash":"0x0000000000000000000000000000000000000000000000000000000000000000",
				"extraData": "0x",
				"gasLimit": "0x0",
				"gasUsed": "0x0",
				"hash": "0xed74541829e559a9256f4810c2358498c7fe41287cb57f4b8b8334ea81560757",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"miner": "0x0000000000000000000000000000000000000000",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000",
				"number": "0x64",
				"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"size": "0x2b9",
				"stateRoot": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"timestamp": "0x0",
				"transactionsRoot": "0x661a9febcfa8f1890af549b874faf9fa274aede26ef489d9db0b25daa569450e",
				"uncles": []
			}`,
		},
		// only tx hashes
		{
			inclTx: true,
			fullTx: false,
			want: `{
				"blockExtraData":"0x",
				"difficulty": "0x0",
				"extDataHash":"0x0000000000000000000000000000000000000000000000000000000000000000",
				"extraData": "0x",
				"gasLimit": "0x0",
				"gasUsed": "0x0",
				"hash": "0xed74541829e559a9256f4810c2358498c7fe41287cb57f4b8b8334ea81560757",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"miner": "0x0000000000000000000000000000000000000000",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000",
				"number": "0x64",
				"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"size": "0x2b9",
				"stateRoot": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"timestamp": "0x0",
				"transactions": [
					"0x7d39df979e34172322c64983a9ad48302c2b889e55bda35324afecf043a77605",
					"0x9bba4c34e57c875ff57ac8d172805a26ae912006985395dc1bdf8f44140a7bf4",
					"0x98909ea1ff040da6be56bc4231d484de1414b3c1dac372d69293a4beb9032cb5",
					"0x12e1f81207b40c3bdcc13c0ee18f5f86af6d31754d57a0ea1b0d4cfef21abef1"
				],
				"transactionsRoot": "0x661a9febcfa8f1890af549b874faf9fa274aede26ef489d9db0b25daa569450e",
				"uncles": []
			}`,
		},
		// full tx details
		{
			inclTx: true,
			fullTx: true,
			want: `{
				"blockExtraData":"0x",
				"difficulty": "0x0",
				"extDataHash":"0x0000000000000000000000000000000000000000000000000000000000000000",
				"extraData": "0x",
				"gasLimit": "0x0",
				"gasUsed": "0x0",
				"hash": "0xed74541829e559a9256f4810c2358498c7fe41287cb57f4b8b8334ea81560757",
				"logsBloom": "0x00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				"miner": "0x0000000000000000000000000000000000000000",
				"mixHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"nonce": "0x0000000000000000",
				"number": "0x64",
				"parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"receiptsRoot": "0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421",
				"sha3Uncles": "0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347",
				"size": "0x2b9",
				"stateRoot": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"timestamp": "0x0",
				"transactions": [
					{
						"blockHash": "0xed74541829e559a9256f4810c2358498c7fe41287cb57f4b8b8334ea81560757",
						"blockNumber": "0x64",
						"from": "0x0000000000000000000000000000000000000000",
						"gas": "0x457",
						"gasPrice": "0x2b67",
						"hash": "0x7d39df979e34172322c64983a9ad48302c2b889e55bda35324afecf043a77605",
						"input": "0x111111",
						"nonce": "0x1",
						"to": "0x0000000000000000000000000000000000000011",
						"transactionIndex": "0x0",
						"value": "0x6f",
						"type": "0x1",
						"accessList": [],
						"chainId": "0x539",
						"v": "0x0",
						"r": "0x0",
						"s": "0x0",
						"yParity": "0x0"
					},
					{
						"blockHash": "0xed74541829e559a9256f4810c2358498c7fe41287cb57f4b8b8334ea81560757",
						"blockNumber": "0x64",
						"from": "0x0000000000000000000000000000000000000000",
						"gas": "0x457",
						"gasPrice": "0x2b67",
						"hash": "0x9bba4c34e57c875ff57ac8d172805a26ae912006985395dc1bdf8f44140a7bf4",
						"input": "0x111111",
						"nonce": "0x2",
						"to": "0x0000000000000000000000000000000000000011",
						"transactionIndex": "0x1",
						"value": "0x6f",
						"type": "0x0",
						"chainId": "0x7fffffffffffffee",
						"v": "0x0",
						"r": "0x0",
						"s": "0x0"
					},
					{
						"blockHash": "0xed74541829e559a9256f4810c2358498c7fe41287cb57f4b8b8334ea81560757",
						"blockNumber": "0x64",
						"from": "0x0000000000000000000000000000000000000000",
						"gas": "0x457",
						"gasPrice": "0x2b67",
						"hash": "0x98909ea1ff040da6be56bc4231d484de1414b3c1dac372d69293a4beb9032cb5",
						"input": "0x111111",
						"nonce": "0x3",
						"to": "0x0000000000000000000000000000000000000011",
						"transactionIndex": "0x2",
						"value": "0x6f",
						"type": "0x1",
						"accessList": [],
						"chainId": "0x539",
						"v": "0x0",
						"r": "0x0",
						"s": "0x0",
						"yParity": "0x0"
					},
					{
						"blockHash": "0xed74541829e559a9256f4810c2358498c7fe41287cb57f4b8b8334ea81560757",
						"blockNumber": "0x64",
						"from": "0x0000000000000000000000000000000000000000",
						"gas": "0x457",
						"gasPrice": "0x2b67",
						"hash": "0x12e1f81207b40c3bdcc13c0ee18f5f86af6d31754d57a0ea1b0d4cfef21abef1",
						"input": "0x111111",
						"nonce": "0x4",
						"to": "0x0000000000000000000000000000000000000011",
						"transactionIndex": "0x3",
						"value": "0x6f",
						"type": "0x0",
						"chainId": "0x7fffffffffffffee",
						"v": "0x0",
						"r": "0x0",
						"s": "0x0"
					}
				],
				"transactionsRoot": "0x661a9febcfa8f1890af549b874faf9fa274aede26ef489d9db0b25daa569450e",
				"uncles": []
			}`,
		},
	}

	for i, tc := range testSuite {
		resp := RPCMarshalBlock(block, tc.inclTx, tc.fullTx, params.TestChainConfig)
		out, err := json.Marshal(resp)
		if err != nil {
			t.Errorf("test %d: json marshal error: %v", i, err)
			continue
		}
		require.JSONEqf(t, tc.want, string(out), "test %d", i)
	}
}

func TestRPCGetBlockOrHeader(t *testing.T) {
	t.Parallel()

	// Initialize test accounts
	var (
		acc1Key, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		acc2Key, _ = crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
		acc1Addr   = crypto.PubkeyToAddress(acc1Key.PublicKey)
		acc2Addr   = crypto.PubkeyToAddress(acc2Key.PublicKey)
		genesis    = &core.Genesis{
			Config: params.TestChainConfig,
			Alloc: types.GenesisAlloc{
				acc1Addr: {Balance: big.NewInt(params.Ether)},
				acc2Addr: {Balance: big.NewInt(params.Ether)},
			},
		}
		genBlocks = 10
		signer    = types.HomesteadSigner{}
		tx        = types.NewTx(&types.LegacyTx{
			Nonce:    11,
			GasPrice: big.NewInt(11111),
			Gas:      1111,
			To:       &acc2Addr,
			Value:    big.NewInt(111),
			Data:     []byte{0x11, 0x11, 0x11},
		})
		pending = types.NewBlock(&types.Header{Number: big.NewInt(11), Time: 42}, []*types.Transaction{tx}, nil, nil, blocktest.NewHasher())
	)
	backend := newTestBackend(t, genBlocks, genesis, dummy.NewFaker(), func(i int, b *core.BlockGen) {
		// Transfer from account[0] to account[1]
		//    value: 1000 wei
		//    fee:   0 wei
		tx, _ := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(i), To: &acc2Addr, Value: big.NewInt(1000), Gas: ethparams.TxGas, GasPrice: b.BaseFee(), Data: nil}), signer, acc1Key)
		b.AddTx(tx)
	})
	api := NewBlockChainAPI(backend)
	blockHashes := make([]common.Hash, genBlocks+1)
	ctx := context.Background()
	for i := 0; i <= genBlocks; i++ {
		header, err := backend.HeaderByNumber(ctx, rpc.BlockNumber(i))
		if err != nil {
			t.Errorf("failed to get block: %d err: %v", i, err)
		}
		blockHashes[i] = header.Hash()
	}
	pendingHash := pending.Hash()

	var testSuite = []struct {
		blockNumber rpc.BlockNumber
		blockHash   *common.Hash
		fullTx      bool
		reqHeader   bool
		file        string
		expectErr   error
	}{
		// 0. latest header
		{
			blockNumber: rpc.LatestBlockNumber,
			reqHeader:   true,
			file:        "tag-latest",
		},
		// 1. genesis header
		{
			blockNumber: rpc.BlockNumber(0),
			reqHeader:   true,
			file:        "number-0",
		},
		// 2. #1 header
		{
			blockNumber: rpc.BlockNumber(1),
			reqHeader:   true,
			file:        "number-1",
		},
		// 3. latest-1 header
		{
			blockNumber: rpc.BlockNumber(9),
			reqHeader:   true,
			file:        "number-latest-1",
		},
		// 4. latest+1 header
		{
			blockNumber: rpc.BlockNumber(11),
			reqHeader:   true,
			file:        "number-latest+1",
		},
		// 5. pending header
		{
			blockNumber: rpc.PendingBlockNumber,
			reqHeader:   true,
			file:        "tag-pending",
		},
		// 6. latest block
		{
			blockNumber: rpc.LatestBlockNumber,
			file:        "tag-latest",
		},
		// 7. genesis block
		{
			blockNumber: rpc.BlockNumber(0),
			file:        "number-0",
		},
		// 8. #1 block
		{
			blockNumber: rpc.BlockNumber(1),
			file:        "number-1",
		},
		// 9. latest-1 block
		{
			blockNumber: rpc.BlockNumber(9),
			fullTx:      true,
			file:        "number-latest-1",
		},
		// 10. latest+1 block
		{
			blockNumber: rpc.BlockNumber(11),
			fullTx:      true,
			file:        "number-latest+1",
		},
		// 11. pending block
		{
			blockNumber: rpc.PendingBlockNumber,
			file:        "tag-pending",
		},
		// 12. pending block + fullTx
		{
			blockNumber: rpc.PendingBlockNumber,
			fullTx:      true,
			file:        "tag-pending-fullTx",
		},
		// 13. latest header by hash
		{
			blockHash: &blockHashes[len(blockHashes)-1],
			reqHeader: true,
			file:      "hash-latest",
		},
		// 14. genesis header by hash
		{
			blockHash: &blockHashes[0],
			reqHeader: true,
			file:      "hash-0",
		},
		// 15. #1 header
		{
			blockHash: &blockHashes[1],
			reqHeader: true,
			file:      "hash-1",
		},
		// 16. latest-1 header
		{
			blockHash: &blockHashes[len(blockHashes)-2],
			reqHeader: true,
			file:      "hash-latest-1",
		},
		// 17. empty hash
		{
			blockHash: &common.Hash{},
			reqHeader: true,
			file:      "hash-empty",
		},
		// 18. pending hash
		{
			blockHash: &pendingHash,
			reqHeader: true,
			file:      `hash-pending`,
		},
		// 19. latest block
		{
			blockHash: &blockHashes[len(blockHashes)-1],
			file:      "hash-latest",
		},
		// 20. genesis block
		{
			blockHash: &blockHashes[0],
			file:      "hash-genesis",
		},
		// 21. #1 block
		{
			blockHash: &blockHashes[1],
			file:      "hash-1",
		},
		// 22. latest-1 block
		{
			blockHash: &blockHashes[len(blockHashes)-2],
			fullTx:    true,
			file:      "hash-latest-1-fullTx",
		},
		// 23. empty hash + body
		{
			blockHash: &common.Hash{},
			fullTx:    true,
			file:      "hash-empty-fullTx",
		},
		// 24. pending block
		{
			blockHash: &pendingHash,
			file:      `hash-pending`,
		},
		// 25. pending block + fullTx
		{
			blockHash: &pendingHash,
			fullTx:    true,
			file:      "hash-pending-fullTx",
		},
	}

	for i, tt := range testSuite {
		var (
			result map[string]interface{}
			err    error
			rpc    string
		)
		if tt.blockHash != nil {
			if tt.reqHeader {
				result = api.GetHeaderByHash(context.Background(), *tt.blockHash)
				rpc = "eth_getHeaderByHash"
			} else {
				result, err = api.GetBlockByHash(context.Background(), *tt.blockHash, tt.fullTx)
				rpc = "eth_getBlockByHash"
			}
		} else {
			if tt.reqHeader {
				result, err = api.GetHeaderByNumber(context.Background(), tt.blockNumber)
				rpc = "eth_getHeaderByNumber"
			} else {
				result, err = api.GetBlockByNumber(context.Background(), tt.blockNumber, tt.fullTx)
				rpc = "eth_getBlockByNumber"
			}
		}
		if tt.expectErr != nil {
			if err == nil {
				t.Errorf("test %d: want error %v, have nothing", i, tt.expectErr)
				continue
			}
			if !errors.Is(err, tt.expectErr) {
				t.Errorf("test %d: error mismatch, want %v, have %v", i, tt.expectErr, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("test %d: want no error, have %v", i, err)
			continue
		}

		testRPCResponseWithFile(t, i, result, rpc, tt.file)
	}
}

func setupReceiptBackend(t *testing.T, genBlocks int) (*testBackend, []common.Hash) {
	config := *params.TestChainConfig
	config.ShanghaiTime = new(uint64)
	config.CancunTime = new(uint64)
	var (
		acc1Key, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		acc2Key, _ = crypto.HexToECDSA("49a7b37aa6f6645917e7b807e9d1c00d4fa71f18343b0d4122a4d2df64dd6fee")
		acc1Addr   = crypto.PubkeyToAddress(acc1Key.PublicKey)
		acc2Addr   = crypto.PubkeyToAddress(acc2Key.PublicKey)
		contract   = common.HexToAddress("0000000000000000000000000000000000031ec7")
		genesis    = &core.Genesis{
			Config:        &config,
			ExcessBlobGas: new(uint64),
			BlobGasUsed:   new(uint64),
			Alloc: types.GenesisAlloc{
				acc1Addr: {Balance: big.NewInt(params.Ether)},
				acc2Addr: {Balance: big.NewInt(params.Ether)},
				// // SPDX-License-Identifier: GPL-3.0
				// pragma solidity >=0.7.0 <0.9.0;
				//
				// contract Token {
				//     event Transfer(address indexed from, address indexed to, uint256 value);
				//     function transfer(address to, uint256 value) public returns (bool) {
				//         emit Transfer(msg.sender, to, value);
				//         return true;
				//     }
				// }
				contract: {Balance: big.NewInt(params.Ether), Code: common.FromHex("0x608060405234801561001057600080fd5b506004361061002b5760003560e01c8063a9059cbb14610030575b600080fd5b61004a6004803603810190610045919061016a565b610060565b60405161005791906101c5565b60405180910390f35b60008273ffffffffffffffffffffffffffffffffffffffff163373ffffffffffffffffffffffffffffffffffffffff167fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef846040516100bf91906101ef565b60405180910390a36001905092915050565b600080fd5b600073ffffffffffffffffffffffffffffffffffffffff82169050919050565b6000610101826100d6565b9050919050565b610111816100f6565b811461011c57600080fd5b50565b60008135905061012e81610108565b92915050565b6000819050919050565b61014781610134565b811461015257600080fd5b50565b6000813590506101648161013e565b92915050565b60008060408385031215610181576101806100d1565b5b600061018f8582860161011f565b92505060206101a085828601610155565b9150509250929050565b60008115159050919050565b6101bf816101aa565b82525050565b60006020820190506101da60008301846101b6565b92915050565b6101e981610134565b82525050565b600060208201905061020460008301846101e0565b9291505056fea2646970667358221220b469033f4b77b9565ee84e0a2f04d496b18160d26034d54f9487e57788fd36d564736f6c63430008120033")},
			},
		}
		signer   = types.LatestSignerForChainID(params.TestChainConfig.ChainID)
		txHashes = make([]common.Hash, genBlocks)
	)

	// FullFaker used to skip header verification that enforces no blobs.
	backend := newTestBackend(t, genBlocks, genesis, dummy.NewFullFaker(), func(i int, b *core.BlockGen) {
		var (
			tx  *types.Transaction
			err error
		)
		// b.SetPoS()
		switch i {
		case 0:
			// transfer 1000wei
			tx, err = types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(i), To: &acc2Addr, Value: big.NewInt(1000), Gas: ethparams.TxGas, GasPrice: b.BaseFee(), Data: nil}), types.HomesteadSigner{}, acc1Key)
		case 1:
			// create contract
			tx, err = types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(i), To: nil, Gas: 53100, GasPrice: b.BaseFee(), Data: common.FromHex("0x60806040")}), signer, acc1Key)
		case 2:
			// with logs
			// transfer(address to, uint256 value)
			data := fmt.Sprintf("0xa9059cbb%s%s", common.HexToHash(common.BigToAddress(big.NewInt(int64(i + 1))).Hex()).String()[2:], common.BytesToHash([]byte{byte(i + 11)}).String()[2:])
			tx, err = types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(i), To: &contract, Gas: 60000, GasPrice: b.BaseFee(), Data: common.FromHex(data)}), signer, acc1Key)
		case 3:
			// dynamic fee with logs
			// transfer(address to, uint256 value)
			data := fmt.Sprintf("0xa9059cbb%s%s", common.HexToHash(common.BigToAddress(big.NewInt(int64(i + 1))).Hex()).String()[2:], common.BytesToHash([]byte{byte(i + 11)}).String()[2:])
			fee := big.NewInt(500)
			fee.Add(fee, b.BaseFee())
			tx, err = types.SignTx(types.NewTx(&types.DynamicFeeTx{Nonce: uint64(i), To: &contract, Gas: 60000, Value: big.NewInt(1), GasTipCap: big.NewInt(500), GasFeeCap: fee, Data: common.FromHex(data)}), signer, acc1Key)
		case 4:
			// access list with contract create
			accessList := types.AccessList{{
				Address:     contract,
				StorageKeys: []common.Hash{{0}},
			}}
			tx, err = types.SignTx(types.NewTx(&types.AccessListTx{Nonce: uint64(i), To: nil, Gas: 58100, GasPrice: b.BaseFee(), Data: common.FromHex("0x60806040"), AccessList: accessList}), signer, acc1Key)
		case 5:
			// blob tx
			fee := big.NewInt(500)
			fee.Add(fee, b.BaseFee())
			tx, err = types.SignTx(types.NewTx(&types.BlobTx{
				Nonce:      uint64(i),
				GasTipCap:  uint256.NewInt(1),
				GasFeeCap:  uint256.MustFromBig(fee),
				Gas:        ethparams.TxGas,
				To:         acc2Addr,
				BlobFeeCap: uint256.NewInt(1),
				BlobHashes: []common.Hash{{1}},
				Value:      new(uint256.Int),
			}), signer, acc1Key)
		}
		if err != nil {
			t.Errorf("failed to sign tx: %v", err)
		}
		if tx != nil {
			b.AddTx(tx)
			txHashes[i] = tx.Hash()
		}
	})
	return backend, txHashes
}

func TestRPCGetTransactionReceipt(t *testing.T) {
	t.Parallel()

	var (
		backend, txHashes = setupReceiptBackend(t, 6)
		api               = NewTransactionAPI(backend, new(AddrLocker))
	)

	var testSuite = []struct {
		txHash common.Hash
		file   string
	}{
		// 0. normal success
		{
			txHash: txHashes[0],
			file:   "normal-transfer-tx",
		},
		// 1. create contract
		{
			txHash: txHashes[1],
			file:   "create-contract-tx",
		},
		// 2. with logs success
		{
			txHash: txHashes[2],
			file:   "with-logs",
		},
		// 3. dynamic tx with logs success
		{
			txHash: txHashes[3],
			file:   `dynamic-tx-with-logs`,
		},
		// 4. access list tx with create contract
		{
			txHash: txHashes[4],
			file:   "create-contract-with-access-list",
		},
		// 5. txhash empty
		{
			txHash: common.Hash{},
			file:   "txhash-empty",
		},
		// 6. txhash not found
		{
			txHash: common.HexToHash("deadbeef"),
			file:   "txhash-notfound",
		},
		// 7. blob tx
		{
			txHash: txHashes[5],
			file:   "blob-tx",
		},
	}

	for i, tt := range testSuite {
		var (
			result interface{}
			err    error
		)
		result, err = api.GetTransactionReceipt(context.Background(), tt.txHash)
		if err != nil {
			t.Errorf("test %d: want no error, have %v", i, err)
			continue
		}
		testRPCResponseWithFile(t, i, result, "eth_getTransactionReceipt", tt.file)
	}
}

func TestRPCGetBlockReceipts(t *testing.T) {
	t.Parallel()

	var (
		genBlocks  = 6
		backend, _ = setupReceiptBackend(t, genBlocks)
		api        = NewBlockChainAPI(backend)
	)
	blockHashes := make([]common.Hash, genBlocks+1)
	ctx := context.Background()
	for i := 0; i <= genBlocks; i++ {
		header, err := backend.HeaderByNumber(ctx, rpc.BlockNumber(i))
		if err != nil {
			t.Errorf("failed to get block: %d err: %v", i, err)
		}
		blockHashes[i] = header.Hash()
	}

	var testSuite = []struct {
		test rpc.BlockNumberOrHash
		file string
	}{
		// 0. block without any txs(hash)
		{
			test: rpc.BlockNumberOrHashWithHash(blockHashes[0], false),
			file: "number-0",
		},
		// 1. block without any txs(number)
		{
			test: rpc.BlockNumberOrHashWithNumber(0),
			file: "number-1",
		},
		// 2. earliest tag
		{
			test: rpc.BlockNumberOrHashWithNumber(rpc.EarliestBlockNumber),
			file: "tag-earliest",
		},
		// 3. latest tag
		{
			test: rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber),
			file: "tag-latest",
		},
		// 4. block with legacy transfer tx(hash)
		{
			test: rpc.BlockNumberOrHashWithHash(blockHashes[1], false),
			file: "block-with-legacy-transfer-tx",
		},
		// 5. block with contract create tx(number)
		{
			test: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(2)),
			file: "block-with-contract-create-tx",
		},
		// 6. block with legacy contract call tx(hash)
		{
			test: rpc.BlockNumberOrHashWithHash(blockHashes[3], false),
			file: "block-with-legacy-contract-call-tx",
		},
		// 7. block with dynamic fee tx(number)
		{
			test: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(4)),
			file: "block-with-dynamic-fee-tx",
		},
		// 8. block is empty
		{
			test: rpc.BlockNumberOrHashWithHash(common.Hash{}, false),
			file: "hash-empty",
		},
		// 9. block is not found
		{
			test: rpc.BlockNumberOrHashWithHash(common.HexToHash("deadbeef"), false),
			file: "hash-notfound",
		},
		// 10. block is not found
		{
			test: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(genBlocks + 1)),
			file: "block-notfound",
		},
		// 11. block with blob tx
		{
			test: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(6)),
			file: "block-with-blob-tx",
		},
	}

	for i, tt := range testSuite {
		var (
			result interface{}
			err    error
		)
		result, err = api.GetBlockReceipts(context.Background(), tt.test)
		if err != nil {
			t.Errorf("test %d: want no error, have %v", i, err)
			continue
		}
		testRPCResponseWithFile(t, i, result, "eth_getBlockReceipts", tt.file)
	}
}

func testRPCResponseWithFile(t *testing.T, testid int, result interface{}, rpc string, file string) {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Errorf("test %d: json marshal error", testid)
		return
	}
	outputFile := filepath.Join("testdata", fmt.Sprintf("%s-%s.json", rpc, file))
	if os.Getenv("WRITE_TEST_FILES") != "" {
		os.WriteFile(outputFile, data, 0644)
	}
	want, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("error reading expected test file: %s output: %v", outputFile, err)
	}
	require.JSONEqf(t, string(want), string(data), "test %d: json not match, want: %s, have: %s", testid, string(want), string(data))
}
