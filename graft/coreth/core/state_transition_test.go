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

package core

import (
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/nativeasset"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap0"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap1"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"
)

var (
	signer     = types.LatestSigner(params.TestChainConfig)
	testKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddr   = crypto.PubkeyToAddress(testKey.PublicKey)
)

func makeContractTx(nonce uint64, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *types.Transaction {
	tx, _ := types.SignTx(types.NewContractCreation(nonce, amount, gasLimit, gasPrice, data), signer, testKey)
	return tx
}

func makeTx(nonce uint64, to common.Address, amount *big.Int, gasLimit uint64, gasPrice *big.Int, data []byte) *types.Transaction {
	tx, _ := types.SignTx(types.NewTransaction(nonce, to, amount, gasLimit, gasPrice, data), signer, testKey)
	return tx
}

/*
//SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

// FromWithinContract creates a contract with a single fallback function that invokes Native Asset Call
// so that a transaction does not need to specify any input data to hit the call.
contract FromWithinContract {
  fallback() external {
    address precompile = 0x0100000000000000000000000000000000000002;
    precompile.call(abi.encodePacked());
  }
}


// FromWithinContractConstructor creates a contract that hits Native Asset Call within the contract constructor.
contract FromWithinContractConstructor {
  constructor () {
    address precompile = 0x0100000000000000000000000000000000000002;
    precompile.call(abi.encodePacked());
  }
}
*/

type stateTransitionTest struct {
	config  *params.ChainConfig
	txs     []*types.Transaction
	gasUsed []uint64
	want    string
}

func executeStateTransitionTest(t *testing.T, st stateTransitionTest) {
	require := require.New(t)

	require.Equal(len(st.txs), len(st.gasUsed), "length of gas used must match length of txs")

	var (
		db    = rawdb.NewMemoryDatabase()
		gspec = &Genesis{
			Config: st.config,
			Alloc: types.GenesisAlloc{
				common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7"): types.Account{
					Balance: big.NewInt(2000000000000000000), // 2 ether
					Nonce:   0,
				},
			},
			GasLimit: ap1.GasLimit,
		}
		genesis       = gspec.ToBlock()
		engine        = dummy.NewFaker()
		blockchain, _ = NewBlockChain(db, DefaultCacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
	)
	defer blockchain.Stop()

	statedb, err := state.New(genesis.Root(), blockchain.stateCache, blockchain.snaps)
	require.NoError(err)

	block := GenerateBadBlock(genesis, engine, st.txs, blockchain.chainConfig)
	receipts, _, _, err := blockchain.processor.Process(block, genesis.Header(), statedb, blockchain.vmConfig)

	if st.want == "" {
		// If no error is expected, require no error and verify the correct gas used amounts from the receipts
		require.NoError(err)

		for i, gasUsed := range st.gasUsed {
			require.Equal(gasUsed, receipts[i].GasUsed, "expected gas used to match for index %d", i)
		}
	} else {
		require.ErrorContains(err, st.want)
	}
}

func TestNativeAssetContractCall(t *testing.T) {
	require := require.New(t)

	data, err := hex.DecodeString("608060405234801561001057600080fd5b5061016e806100206000396000f3fe608060405234801561001057600080fd5b50600073010000000000000000000000000000000000000290508073ffffffffffffffffffffffffffffffffffffffff166040516020016040516020818303038152906040526040516100639190610121565b6000604051808303816000865af19150503d80600081146100a0576040519150601f19603f3d011682016040523d82523d6000602084013e6100a5565b606091505b005b600081519050919050565b600081905092915050565b60005b838110156100db5780820151818401526020810190506100c0565b838111156100ea576000848401525b50505050565b60006100fb826100a7565b61010581856100b2565b93506101158185602086016100bd565b80840191505092915050565b600061012d82846100f0565b91508190509291505056fea2646970667358221220a297c3e133143287abef22b1c1d4e45f588efc3db99a84b364560a2079876fc364736f6c634300080d0033")
	require.NoError(err)

	contractAddr := crypto.CreateAddress(testAddr, 0)
	txs := []*types.Transaction{
		makeContractTx(0, common.Big0, 500_000, big.NewInt(ap0.MinGasPrice), data),
		makeTx(1, contractAddr, common.Big0, 100_000, big.NewInt(ap0.MinGasPrice), nil), // No input data is necessary, since this will hit the contract's fallback function.
	}

	tests := map[string]stateTransitionTest{
		"phase5": {
			config:  params.TestApricotPhase5Config,
			txs:     txs,
			gasUsed: []uint64{132091, 41618},
			want:    "",
		},
		"prePhase6": {
			config:  params.TestApricotPhasePre6Config,
			txs:     txs,
			gasUsed: []uint64{132091, 21618},
			want:    "",
		},
		"phase6": {
			config:  params.TestApricotPhase6Config,
			txs:     txs,
			gasUsed: []uint64{132091, 41618},
			want:    "",
		},
		"banff": {
			config:  params.TestBanffChainConfig,
			txs:     txs,
			gasUsed: []uint64{132091, 21618},
			want:    "",
		},
		"durango": {
			config:  params.TestDurangoChainConfig,
			txs:     txs,
			gasUsed: []uint64{132117, 21618},
			want:    "",
		},
		"etna": {
			config:  params.TestEtnaChainConfig,
			txs:     txs,
			gasUsed: []uint64{132117, 21618},
			want:    "",
		},
	}

	for name, stTest := range tests {
		t.Run(name, func(t *testing.T) {
			executeStateTransitionTest(t, stTest)
		})
	}
}

func TestNativeAssetContractConstructor(t *testing.T) {
	require := require.New(t)

	data, err := hex.DecodeString("608060405234801561001057600080fd5b50600073010000000000000000000000000000000000000290508073ffffffffffffffffffffffffffffffffffffffff166040516020016040516020818303038152906040526040516100639190610128565b6000604051808303816000865af19150503d80600081146100a0576040519150601f19603f3d011682016040523d82523d6000602084013e6100a5565b606091505b5050505061013f565b600081519050919050565b600081905092915050565b60005b838110156100e25780820151818401526020810190506100c7565b838111156100f1576000848401525b50505050565b6000610102826100ae565b61010c81856100b9565b935061011c8185602086016100c4565b80840191505092915050565b600061013482846100f7565b915081905092915050565b603f8061014d6000396000f3fe6080604052600080fdfea26469706673582212208a8a2e0bb031a4d5bdfa861a6e43ae57e6f4e0cc40d069ad6f52585406790ac864736f6c634300080d0033")
	require.NoError(err)

	txs := []*types.Transaction{
		makeContractTx(0, common.Big0, 100_000, big.NewInt(ap0.MinGasPrice), data),
	}

	phase6Tests := map[string]stateTransitionTest{
		"phase5": {
			config:  params.TestApricotPhase5Config,
			txs:     txs,
			gasUsed: []uint64{92046},
			want:    "",
		},
		"prePhase6": {
			config:  params.TestApricotPhasePre6Config,
			txs:     txs,
			gasUsed: []uint64{72046},
			want:    "",
		},
		"phase6": {
			config:  params.TestApricotPhase6Config,
			txs:     txs,
			gasUsed: []uint64{92046},
			want:    "",
		},
		"banff": {
			config:  params.TestBanffChainConfig,
			txs:     txs,
			gasUsed: []uint64{72046},
			want:    "",
		},
	}

	for name, stTest := range phase6Tests {
		t.Run(name, func(t *testing.T) {
			executeStateTransitionTest(t, stTest)
		})
	}
}

func TestNativeAssetDirectEOACall(t *testing.T) {
	txs := []*types.Transaction{
		makeTx(0, nativeasset.NativeAssetCallAddr, common.Big0, 100_000, big.NewInt(ap0.MinGasPrice), nil),
	}

	phase6Tests := map[string]stateTransitionTest{
		"phase5": {
			config:  params.TestApricotPhase5Config,
			txs:     txs,
			gasUsed: []uint64{41000},
			want:    "",
		},
		// Note: PrePhase6 used a soft error to ensure the Native Asset Call precompile was not used from an EOA, however,
		// after PrePhase6 was over, this soft error was no longer needed since it would never be included in the chain, so
		// it has been removed.
		// Therefore, there is no need for an error to be returned in this test case even though a soft error would have been
		// returned during PrePhase6.
		"prePhase6": {
			config:  params.TestApricotPhasePre6Config,
			txs:     txs,
			gasUsed: []uint64{21000},
			want:    "",
		},
		"phase6": {
			config:  params.TestApricotPhase6Config,
			txs:     txs,
			gasUsed: []uint64{41000},
			want:    "",
		},
		"banff": {
			config:  params.TestBanffChainConfig,
			txs:     txs,
			gasUsed: []uint64{21000},
			want:    "",
		},
	}

	for name, stTest := range phase6Tests {
		t.Run(name, func(t *testing.T) {
			executeStateTransitionTest(t, stTest)
		})
	}
}
