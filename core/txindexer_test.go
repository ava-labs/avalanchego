// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
// Copyright 2024 The go-ethereum Authors
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
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>

package core

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	ethparams "github.com/ava-labs/libevm/params"
	"github.com/ava-labs/subnet-evm/consensus/dummy"
	"github.com/ava-labs/subnet-evm/core/coretest"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/stretchr/testify/require"
)

// Should we try to use the TestTxIndexer from upstream here instead
// or move this test to a new file eg, blockchain_extra_test.go?
func TestTransactionIndices(t *testing.T) {
	// Configure and generate a sample block chain
	require := require.New(t)
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		funds   = big.NewInt(10000000000000)
		gspec   = &Genesis{
			Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
			Alloc:  GenesisAlloc{addr1: {Balance: funds}},
		}
		signer = types.LatestSigner(gspec.Config)
	)
	genDb, blocks, _, err := GenerateChainWithGenesis(gspec, dummy.NewFaker(), 128, 10, func(i int, block *BlockGen) {
		tx, err := types.SignTx(types.NewTransaction(block.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		require.NoError(err)
		block.AddTx(tx)
	})
	require.NoError(err)

	blocks2, _, err := GenerateChain(gspec.Config, blocks[len(blocks)-1], dummy.NewFaker(), genDb, 10, 10, func(i int, block *BlockGen) {
		tx, err := types.SignTx(types.NewTransaction(block.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		require.NoError(err)
		block.AddTx(tx)
	})
	require.NoError(err)

	conf := &CacheConfig{
		TrieCleanLimit:            256,
		TrieDirtyLimit:            256,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 4,
		Pruning:                   true,
		CommitInterval:            4096,
		SnapshotLimit:             256,
		SnapshotNoBuild:           true, // Ensure the test errors if snapshot initialization fails
		AcceptorQueueLimit:        64,
	}

	// Init block chain and check all needed indices has been indexed.
	chainDB := rawdb.NewMemoryDatabase()
	chain, err := createBlockChain(chainDB, conf, gspec, common.Hash{})
	require.NoError(err)

	_, err = chain.InsertChain(blocks)
	require.NoError(err)

	for _, block := range blocks {
		err := chain.Accept(block)
		require.NoError(err)
	}
	chain.DrainAcceptorQueue()

	lastAcceptedBlock := blocks[len(blocks)-1]
	require.Equal(lastAcceptedBlock.Hash(), chain.CurrentHeader().Hash())

	coretest.CheckTxIndices(t, nil, 0, lastAcceptedBlock.NumberU64(), lastAcceptedBlock.NumberU64(), chain.db, false) // check all indices has been indexed
	chain.Stop()

	// Reconstruct a block chain which only reserves limited tx indices
	// 128 blocks were previously indexed. Now we add a new block at each test step.
	limits := []uint64{
		0,   /* tip: 129 reserve all (don't run) */
		131, /* tip: 130 reserve all */
		140, /* tip: 131 reserve all */
		64,  /* tip: 132, limit:64 */
		32,  /* tip: 133, limit:32  */
	}
	for i, l := range limits {
		t.Run(fmt.Sprintf("test-%d, limit: %d", i+1, l), func(t *testing.T) {
			conf.TransactionHistory = l

			chain, err := createBlockChain(chainDB, conf, gspec, lastAcceptedBlock.Hash())
			require.NoError(err)

			tail := getTail(l, lastAcceptedBlock.NumberU64())
			var indexedFrom uint64
			if tail != nil {
				indexedFrom = *tail
			}
			// check if startup indices are correct
			coretest.CheckTxIndices(t, tail, indexedFrom, lastAcceptedBlock.NumberU64(), lastAcceptedBlock.NumberU64(), chain.db, false)

			newBlks := blocks2[i : i+1]
			_, err = chain.InsertChain(newBlks) // Feed chain a higher block to trigger indices updater.
			require.NoError(err)

			lastAcceptedBlock = newBlks[0]
			err = chain.Accept(lastAcceptedBlock) // Accept the block to trigger indices updater.
			require.NoError(err)
			chain.DrainAcceptorQueue()

			tail = getTail(l, lastAcceptedBlock.NumberU64())
			indexedFrom = uint64(0)
			if tail != nil {
				indexedFrom = *tail
			}
			// check if indices are updated correctly
			coretest.CheckTxIndices(t, tail, indexedFrom, lastAcceptedBlock.NumberU64(), lastAcceptedBlock.NumberU64(), chain.db, false)
			chain.Stop()
		})
	}
}

func getTail(limit uint64, lastAccepted uint64) *uint64 {
	if limit == 0 {
		return nil
	}
	var tail uint64
	if lastAccepted > limit {
		// tail should be the oldest block number which is indexed
		// i.e the first block number that's in the lookup range
		tail = lastAccepted - limit + 1
	}
	return &tail
}

func TestTransactionSkipIndexing(t *testing.T) {
	// Configure and generate a sample block chain
	require := require.New(t)
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		funds   = big.NewInt(10000000000000)
		gspec   = &Genesis{
			Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
			Alloc:  GenesisAlloc{addr1: {Balance: funds}},
		}
		signer = types.LatestSigner(gspec.Config)
	)
	genDb, blocks, _, err := GenerateChainWithGenesis(gspec, dummy.NewCoinbaseFaker(), 5, 10, func(i int, block *BlockGen) {
		tx, err := types.SignTx(types.NewTransaction(block.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		require.NoError(err)
		block.AddTx(tx)
	})
	require.NoError(err)

	blocks2, _, err := GenerateChain(gspec.Config, blocks[len(blocks)-1], dummy.NewCoinbaseFaker(), genDb, 5, 10, func(i int, block *BlockGen) {
		tx, err := types.SignTx(types.NewTransaction(block.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		require.NoError(err)
		block.AddTx(tx)
	})
	require.NoError(err)

	conf := &CacheConfig{
		TrieCleanLimit:            256,
		TrieDirtyLimit:            256,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 4,
		Pruning:                   true,
		CommitInterval:            4096,
		SnapshotLimit:             256,
		SnapshotNoBuild:           true, // Ensure the test errors if snapshot initialization fails
		AcceptorQueueLimit:        64,
		SkipTxIndexing:            true,
	}

	// test1: Init block chain and check all indices has been skipped.
	chainDB := rawdb.NewMemoryDatabase()
	chain, err := createAndInsertChain(chainDB, conf, gspec, blocks, common.Hash{},
		func(b *types.Block) {
			bNumber := b.NumberU64()
			coretest.CheckTxIndices(t, nil, bNumber+1, bNumber+1, bNumber, chainDB, false) // check all indices has been skipped
		})
	require.NoError(err)
	chain.Stop()

	// test2: specify lookuplimit with tx index skipping enabled. Blocks should not be indexed but tail should be updated.
	conf.TransactionHistory = 2
	chainDB = rawdb.NewMemoryDatabase()
	chain, err = createAndInsertChain(chainDB, conf, gspec, blocks, common.Hash{},
		func(b *types.Block) {
			bNumber := b.NumberU64()
			tail := bNumber - conf.TransactionHistory + 1
			coretest.CheckTxIndices(t, &tail, bNumber+1, bNumber+1, bNumber, chainDB, false) // check all indices has been skipped
		})
	require.NoError(err)
	chain.Stop()

	// test3: tx index skipping and unindexer disabled. Blocks should be indexed and tail should be updated.
	conf.TransactionHistory = 0
	conf.SkipTxIndexing = false
	chainDB = rawdb.NewMemoryDatabase()
	chain, err = createAndInsertChain(chainDB, conf, gspec, blocks, common.Hash{},
		func(b *types.Block) {
			bNumber := b.NumberU64()
			coretest.CheckTxIndices(t, nil, 0, bNumber, bNumber, chainDB, false) // check all indices has been indexed
		})
	require.NoError(err)
	chain.Stop()

	// now change tx index skipping to true and check that the indices are skipped for the last block
	// and old indices are removed up to the tail, but [tail, current) indices are still there.
	conf.TransactionHistory = 2
	conf.SkipTxIndexing = true
	chain, err = createAndInsertChain(chainDB, conf, gspec, blocks2[0:1], chain.CurrentHeader().Hash(),
		func(b *types.Block) {
			bNumber := b.NumberU64()
			tail := bNumber - conf.TransactionHistory + 1
			coretest.CheckTxIndices(t, &tail, tail, bNumber-1, bNumber, chainDB, false)
		})
	require.NoError(err)
	chain.Stop()
}

func createAndInsertChain(db ethdb.Database, cacheConfig *CacheConfig, gspec *Genesis, blocks types.Blocks, lastAcceptedHash common.Hash, accepted func(*types.Block)) (*BlockChain, error) {
	chain, err := createBlockChain(db, cacheConfig, gspec, lastAcceptedHash)
	if err != nil {
		return nil, err
	}
	_, err = chain.InsertChain(blocks)
	if err != nil {
		return nil, err
	}
	for _, block := range blocks {
		err := chain.Accept(block)
		if err != nil {
			return nil, err
		}
		chain.DrainAcceptorQueue()
		if accepted != nil {
			accepted(block)
		}
	}

	return chain, nil
}
