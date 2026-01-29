// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/upgrade/ap3"
)

func TestAcceptedLogsSubscription(t *testing.T) {
	/*
		Example contract to test event emission:

			pragma solidity >=0.7.0 <0.9.0;
			contract Callable {
				event Called();
				function Call() public { emit Called(); }
			}
	*/

	const (
		callableABI = "[{\"anonymous\":false,\"inputs\":[],\"name\":\"Called\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"Call\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]"
		callableBin = "6080604052348015600f57600080fd5b5060998061001e6000396000f3fe6080604052348015600f57600080fd5b506004361060285760003560e01c806334e2292114602d575b600080fd5b60336035565b005b7f81fab7a4a0aa961db47eefc81f143a5220e8c8495260dd65b1356f1d19d3c7b860405160405180910390a156fea2646970667358221220029436d24f3ac598ceca41d4d712e13ced6d70727f4cdc580667de66d2f51d8b64736f6c63430008010033"
	)
	var (
		require = require.New(t)
		engine  = dummy.NewCoinbaseFaker()
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		funds   = new(big.Int).Mul(big.NewInt(100), big.NewInt(params.Ether))
		gspec   = &Genesis{
			Config:  params.TestChainConfig,
			Alloc:   types.GenesisAlloc{addr1: {Balance: funds}},
			BaseFee: big.NewInt(ap3.InitialBaseFee),
		}
		contractAddress = crypto.CreateAddress(addr1, 0)
		signer          = types.LatestSigner(gspec.Config)
	)

	parsed, err := abi.JSON(strings.NewReader(callableABI))
	require.NoError(err)

	packedFunction, err := parsed.Pack("Call")
	require.NoError(err)

	_, blocks, _, err := GenerateChainWithGenesis(gspec, engine, 2, 10, func(i int, b *BlockGen) {
		switch i {
		case 0:
			// First, we deploy the contract
			contractTx := types.NewContractCreation(0, common.Big0, 200000, big.NewInt(ap3.InitialBaseFee), common.FromHex(callableBin))
			contractSignedTx, err := types.SignTx(contractTx, signer, key1)
			require.NoError(err)
			b.AddTx(contractSignedTx)
		case 1:
			// In the next block, we call the contract function
			tx := types.NewTransaction(1, contractAddress, common.Big0, 23000, big.NewInt(ap3.InitialBaseFee), packedFunction)
			tx, err := types.SignTx(tx, signer, key1)
			require.NoError(err)
			b.AddTx(tx)
		}
	})
	require.NoError(err)

	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), DefaultCacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
	require.NoError(err)
	defer chain.Stop()

	// Create Log Subscriber
	logsCh := make(chan []*types.Log, 10)
	defer close(logsCh)

	sub := chain.SubscribeAcceptedLogsEvent(logsCh)
	defer sub.Unsubscribe()

	_, err = chain.InsertChain(blocks)
	require.NoError(err)

	for _, block := range blocks {
		require.NoError(chain.Accept(block))
	}
	chain.DrainAcceptorQueue()

	logs := <-logsCh
	require.Len(logs, 1)
	require.Equal(blocks[1].Hash(), logs[0].BlockHash)
	require.Equal(blocks[1].Number().Uint64(), logs[0].BlockNumber)
}
