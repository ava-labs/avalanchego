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
// Copyright 2014 The go-ethereum Authors
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
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/state/pruner"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/customrawdb"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap3"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/eth/tracers/logger"
	"github.com/ava-labs/libevm/ethdb"
	ethparams "github.com/ava-labs/libevm/params"
)

var (
	archiveConfig = &CacheConfig{
		TrieCleanLimit:            256,
		TrieDirtyLimit:            256,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 4,
		Pruning:                   false, // Archive mode
		SnapshotLimit:             256,
		AcceptorQueueLimit:        64,
	}

	pruningConfig = &CacheConfig{
		TrieCleanLimit:            256,
		TrieDirtyLimit:            256,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 4,
		Pruning:                   true, // Enable pruning
		CommitInterval:            4096,
		StateHistory:              32,
		SnapshotLimit:             256,
		AcceptorQueueLimit:        64,
	}

	// Firewood should only be included for non-archive, snapshot disabled tests.
	schemes = []string{rawdb.HashScheme, customrawdb.FirewoodScheme}
)

func newGwei(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(params.GWei))
}

func createBlockChain(
	db ethdb.Database,
	cacheConfig *CacheConfig,
	gspec *Genesis,
	lastAcceptedHash common.Hash,
) (*BlockChain, error) {
	// Import the chain. This runs all block validation rules.
	blockchain, err := NewBlockChain(
		db,
		cacheConfig,
		gspec,
		dummy.NewFakerWithCallbacks(TestCallbacks),
		vm.Config{},
		lastAcceptedHash,
		false,
	)
	return blockchain, err
}

func TestArchiveBlockChain(t *testing.T) {
	createArchiveBlockChain := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, _ string) (*BlockChain, error) {
		return createBlockChain(db, archiveConfig, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, createArchiveBlockChain)
		})
	}
}

func TestArchiveBlockChainSnapsDisabled(t *testing.T) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, _ string) (*BlockChain, error) {
		return createBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit:            256,
				TrieDirtyLimit:            256,
				TrieDirtyCommitTarget:     20,
				TriePrefetcherParallelism: 4,
				Pruning:                   false, // Archive mode
				SnapshotLimit:             0,     // Disable snapshots
				AcceptorQueueLimit:        64,
			},
			gspec,
			lastAcceptedHash,
		)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestPruningBlockChain(t *testing.T) {
	createPruningBlockChain := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, _ string) (*BlockChain, error) {
		return createBlockChain(db, pruningConfig, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, createPruningBlockChain)
		})
	}
}

func TestPruningBlockChainSnapsDisabled(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testPruningBlockChainSnapsDisabled(t, scheme)
		})
	}
}

func testPruningBlockChainSnapsDisabled(t *testing.T, scheme string) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, dataPath string) (*BlockChain, error) {
		return createBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit:            256,
				TrieDirtyLimit:            256,
				TrieDirtyCommitTarget:     20,
				TriePrefetcherParallelism: 4,
				Pruning:                   true, // Enable pruning
				CommitInterval:            4096,
				StateHistory:              32,
				SnapshotLimit:             0, // Disable snapshots
				AcceptorQueueLimit:        64,
				StateScheme:               scheme,
				ChainDataDir:              dataPath,
			},
			gspec,
			lastAcceptedHash,
		)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

// Runs the same tests but ensures that the blockchain can handle eth blocks
// that are empty, without atomic txs. This can happen during bootstrapping.
func TestPruningEmptyCallbacks(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testPruningEmptyCallbacks(t, scheme)
		})
	}
}

func testPruningEmptyCallbacks(t *testing.T, scheme string) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, dataPath string) (*BlockChain, error) {
		cacheConfig := &CacheConfig{
			TrieCleanLimit:            256,
			TrieDirtyLimit:            256,
			TrieDirtyCommitTarget:     20,
			TriePrefetcherParallelism: 4,
			Pruning:                   true,
			CommitInterval:            4096,
			SnapshotLimit:             0, // Disable snapshots
			AcceptorQueueLimit:        64,
			StateScheme:               scheme,
			StateHistory:              32,
			ChainDataDir:              dataPath,
		}
		blockchain, err := NewBlockChain(
			db,
			cacheConfig,
			gspec,
			dummy.NewFakerWithCallbacks(TestEmptyCallbacks),
			vm.Config{},
			lastAcceptedHash,
			false,
		)
		if err != nil {
			return nil, err
		}
		return blockchain, nil
	}

	// Only need to test the ones with empty blocks.
	tests := []ChainTest{
		{
			"EmptyBlocks",
			EmptyBlocksTest,
		},
		{
			"EmptyAndNonEmptyBlocks",
			EmptyAndNonEmptyBlocksTest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

type wrappedStateManager struct {
	TrieWriter
}

func (w *wrappedStateManager) Shutdown() error { return nil }

func TestPruningBlockChainUngracefulShutdown(t *testing.T) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, _ string) (*BlockChain, error) {
		blockchain, err := createBlockChain(db, pruningConfig, gspec, lastAcceptedHash)
		if err != nil {
			return nil, err
		}

		// Overwrite state manager, so that Shutdown is not called.
		// This tests to ensure that the state manager handles an ungraceful shutdown correctly.
		blockchain.stateManager = &wrappedStateManager{TrieWriter: blockchain.stateManager}
		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestPruningBlockChainUngracefulShutdownSnapsDisabled(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testPruningBlockChainUngracefulShutdownSnapsDisabled(t, scheme)
		})
	}
}

func testPruningBlockChainUngracefulShutdownSnapsDisabled(t *testing.T, scheme string) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, dataPath string) (*BlockChain, error) {
		// If no database path has been persisted, set the path to a temporary test directory.
		// Otherwise, make sure not to overwrite so that it re-uses any existing database.
		blockchain, err := createBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit:            256,
				TrieDirtyLimit:            256,
				TrieDirtyCommitTarget:     20,
				TriePrefetcherParallelism: 4,
				Pruning:                   true, // Enable pruning
				CommitInterval:            4096,
				StateHistory:              32,
				SnapshotLimit:             0, // Disable snapshots
				AcceptorQueueLimit:        64,
				StateScheme:               scheme,
				ChainDataDir:              dataPath,
			},
			gspec,
			lastAcceptedHash,
		)
		if err != nil {
			return nil, err
		}

		// Overwrite state manager, so that Shutdown is not called.
		// This tests to ensure that the state manager handles an ungraceful shutdown correctly.
		blockchain.stateManager = &wrappedStateManager{TrieWriter: blockchain.stateManager}
		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestEnableSnapshots(t *testing.T) {
	// Set snapshots to be disabled the first time, and then enable them on the restart
	snapLimit := 0
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, _ string) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := createBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit:            256,
				TrieDirtyLimit:            256,
				TrieDirtyCommitTarget:     20,
				TriePrefetcherParallelism: 4,
				Pruning:                   true, // Enable pruning
				CommitInterval:            4096,
				StateHistory:              32,
				SnapshotLimit:             snapLimit,
				AcceptorQueueLimit:        64,
			},
			gspec,
			lastAcceptedHash,
		)
		if err != nil {
			return nil, err
		}
		snapLimit = 256

		return blockchain, err
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestCorruptSnapshots(t *testing.T) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, _ string) (*BlockChain, error) {
		// Delete the snapshot block hash and state root to ensure that if we die in between writing a snapshot
		// diff layer to disk at any point, we can still recover on restart.
		customrawdb.DeleteSnapshotBlockHash(db)
		rawdb.DeleteSnapshotRoot(db)

		return createBlockChain(db, pruningConfig, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestBlockChainOfflinePruningUngracefulShutdown(t *testing.T) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, _ string) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := createBlockChain(db, pruningConfig, gspec, lastAcceptedHash)
		if err != nil {
			return nil, err
		}

		// Overwrite state manager, so that Shutdown is not called.
		// This tests to ensure that the state manager handles an ungraceful shutdown correctly.
		blockchain.stateManager = &wrappedStateManager{TrieWriter: blockchain.stateManager}

		if lastAcceptedHash == (common.Hash{}) {
			return blockchain, nil
		}

		if err := blockchain.CleanBlockRootsAboveLastAccepted(); err != nil {
			return nil, err
		}
		// get the target root to prune to before stopping the blockchain
		targetRoot := blockchain.LastAcceptedBlock().Root()
		blockchain.Stop()

		tempDir := t.TempDir()
		prunerConfig := pruner.Config{
			Datadir:   tempDir,
			BloomSize: 256,
		}

		pruner, err := pruner.NewPruner(db, prunerConfig)
		if err != nil {
			return nil, fmt.Errorf("offline pruning failed (%s, %d): %w", tempDir, 256, err)
		}

		if err := pruner.Prune(targetRoot); err != nil {
			return nil, fmt.Errorf("failed to prune blockchain with target root: %s due to: %w", targetRoot, err)
		}
		// Re-initialize the blockchain after pruning
		return createBlockChain(db, pruningConfig, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			tt.testFunc(t, create)
		})
	}
}

func testRepopulateMissingTriesParallel(t *testing.T, parallelism int) {
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  types.GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := createBlockChain(chainDB, pruningConfig, gspec, common.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	defer blockchain.Stop()

	// This call generates a chain of 3 blocks.
	signer := types.HomesteadSigner{}
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 10, 10, func(i int, gen *BlockGen) {
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), ethparams.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := blockchain.InsertChain(chain); err != nil {
		t.Fatal(err)
	}
	for _, block := range chain {
		if err := blockchain.Accept(block); err != nil {
			t.Fatal(err)
		}
	}
	blockchain.DrainAcceptorQueue()

	lastAcceptedHash := blockchain.LastConsensusAcceptedBlock().Hash()
	blockchain.Stop()

	blockchain, err = createBlockChain(chainDB, pruningConfig, gspec, lastAcceptedHash)
	if err != nil {
		t.Fatal(err)
	}

	// Confirm that the node does not have the state for intermediate nodes (exclude the last accepted block)
	for _, block := range chain[:len(chain)-1] {
		if blockchain.HasState(block.Root()) {
			t.Fatalf("Expected blockchain to be missing state for intermediate block %d with pruning enabled", block.NumberU64())
		}
	}
	blockchain.Stop()

	startHeight := uint64(1)
	// Create a node in archival mode and re-populate the trie history.
	blockchain, err = createBlockChain(
		chainDB,
		&CacheConfig{
			TrieCleanLimit:                  256,
			TrieDirtyLimit:                  256,
			TrieDirtyCommitTarget:           20,
			TriePrefetcherParallelism:       4,
			Pruning:                         false, // Archive mode
			SnapshotLimit:                   256,
			PopulateMissingTries:            &startHeight, // Starting point for re-populating.
			PopulateMissingTriesParallelism: parallelism,
			AcceptorQueueLimit:              64,
		},
		gspec,
		lastAcceptedHash,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer blockchain.Stop()

	for _, block := range chain {
		if !blockchain.HasState(block.Root()) {
			t.Fatalf("failed to re-generate state for block %d", block.NumberU64())
		}
	}
}

func TestRepopulateMissingTries(t *testing.T) {
	// Test with different levels of parallelism as a regression test.
	for _, parallelism := range []int{1, 2, 4, 1024} {
		testRepopulateMissingTriesParallel(t, parallelism)
	}
}

func TestUngracefulAsyncShutdown(t *testing.T) {
	testUngracefulAsyncShutdown(t, rawdb.HashScheme, true)
}

// HashDB passes these tests because:
// lastAcceptedHeight <= lastCommittedHeight + 2 * commitInterval
// where lastCommittedHeight is the last multiple of commitInterval (so 0)
// Firewood passes these tests because lastCommittedHeight always equals acceptorTip.
// This means it will work as long as lastAcceptedHeight <= acceptorTip + 2 * commitInterval
func TestUngracefulAsyncShutdownNoSnapshots(t *testing.T) {
	for _, scheme := range schemes {
		t.Run(scheme, func(t *testing.T) {
			testUngracefulAsyncShutdown(t, scheme, false)
		})
	}
}

func testUngracefulAsyncShutdown(t *testing.T, scheme string, snapshotEnabled bool) {
	snapshotLimit := 0
	if snapshotEnabled {
		snapshotLimit = 256
	}
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, dataPath string, commitInterval uint64) (*BlockChain, error) {
		blockchain, err := createBlockChain(db, &CacheConfig{
			TrieCleanLimit:            256,
			TrieDirtyLimit:            256,
			TrieDirtyCommitTarget:     20,
			TriePrefetcherParallelism: 4,
			Pruning:                   true,
			CommitInterval:            commitInterval,
			StateScheme:               scheme,
			ChainDataDir:              dataPath,
			StateHistory:              32,
			SnapshotLimit:             snapshotLimit,
			SnapshotNoBuild:           snapshotEnabled, // If true, ensure that the test errors if snapshot initialization fails
			AcceptorQueueLimit:        1000,            // ensure channel doesn't block
		}, gspec, lastAcceptedHash)
		if err != nil {
			return nil, err
		}
		return blockchain, nil
	}
	for _, tt := range reexecTests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

// TestCanonicalHashMarker tests all the canonical hash markers are updated/deleted
// correctly in case reorg is called.
func TestCanonicalHashMarker(t *testing.T) {
	for _, scheme := range []string{rawdb.HashScheme, rawdb.PathScheme, customrawdb.FirewoodScheme} {
		t.Run(scheme, func(t *testing.T) {
			testCanonicalHashMarker(t, scheme)
		})
	}
}

func testCanonicalHashMarker(t *testing.T, scheme string) {
	var cases = []struct {
		forkA int
		forkB int
	}{
		// ForkA: 10 blocks
		// ForkB: 1 blocks
		//
		// reorged:
		//      markers [2, 10] should be deleted
		//      markers [1] should be updated
		{10, 1},

		// ForkA: 10 blocks
		// ForkB: 2 blocks
		//
		// reorged:
		//      markers [3, 10] should be deleted
		//      markers [1, 2] should be updated
		{10, 2},

		// ForkA: 10 blocks
		// ForkB: 10 blocks
		//
		// reorged:
		//      markers [1, 10] should be updated
		{10, 10},

		// ForkA: 10 blocks
		// ForkB: 11 blocks
		//
		// reorged:
		//      markers [1, 11] should be updated
		{10, 11},
	}
	for _, c := range cases {
		var (
			gspec = &Genesis{
				Config:  params.TestChainConfig,
				Alloc:   types.GenesisAlloc{},
				BaseFee: big.NewInt(ap3.InitialBaseFee),
			}
			engine = dummy.NewCoinbaseFaker()
		)
		_, forkA, _, err := GenerateChainWithGenesis(gspec, engine, c.forkA, 10, func(i int, gen *BlockGen) {})
		if err != nil {
			t.Fatal(err)
		}
		_, forkB, _, err := GenerateChainWithGenesis(gspec, engine, c.forkB, 10, func(i int, gen *BlockGen) {})
		if err != nil {
			t.Fatal(err)
		}

		// Initialize test chain
		db := rawdb.NewMemoryDatabase()
		cacheConfig := DefaultCacheConfigWithScheme(scheme)
		cacheConfig.ChainDataDir = t.TempDir()
		chain, err := NewBlockChain(db, cacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
		if err != nil {
			t.Fatalf("failed to create tester chain: %v", err)
		}
		// Insert forkA and forkB, the canonical should on forkA still
		if n, err := chain.InsertChain(forkA); err != nil {
			t.Fatalf("block %d: failed to insert into chain: %v", n, err)
		}
		if n, err := chain.InsertChain(forkB); err != nil {
			t.Fatalf("block %d: failed to insert into chain: %v", n, err)
		}

		verify := func(head *types.Block) {
			if chain.CurrentBlock().Hash() != head.Hash() {
				t.Fatalf("Unexpected block hash, want %x, got %x", head.Hash(), chain.CurrentBlock().Hash())
			}
			if chain.CurrentHeader().Hash() != head.Hash() {
				t.Fatalf("Unexpected head header, want %x, got %x", head.Hash(), chain.CurrentHeader().Hash())
			}
			if !chain.HasState(head.Root()) {
				t.Fatalf("Lost block state %v %x", head.Number(), head.Hash())
			}
		}

		// Switch canonical chain to forkB if necessary
		if len(forkA) < len(forkB) {
			verify(forkB[len(forkB)-1])
		} else {
			verify(forkA[len(forkA)-1])
			if err := chain.SetPreference(forkB[len(forkB)-1]); err != nil {
				t.Fatal(err)
			}
			verify(forkB[len(forkB)-1])
		}

		// Ensure all hash markers are updated correctly
		for i := 0; i < len(forkB); i++ {
			block := forkB[i]
			hash := chain.GetCanonicalHash(block.NumberU64())
			if hash != block.Hash() {
				t.Fatalf("Unexpected canonical hash %d", block.NumberU64())
			}
		}
		if c.forkA > c.forkB {
			for i := uint64(c.forkB) + 1; i <= uint64(c.forkA); i++ {
				hash := chain.GetCanonicalHash(i)
				if hash != (common.Hash{}) {
					t.Fatalf("Unexpected canonical hash %d", i)
				}
			}
		}
		chain.Stop()
	}
}

func TestTxLookupBlockChain(t *testing.T) {
	cacheConf := &CacheConfig{
		TrieCleanLimit:            256,
		TrieDirtyLimit:            256,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 4,
		Pruning:                   true,
		CommitInterval:            4096,
		SnapshotLimit:             256,
		StateHistory:              32,
		SnapshotNoBuild:           true, // Ensure the test errors if snapshot initialization fails
		AcceptorQueueLimit:        64,   // ensure channel doesn't block
		TransactionHistory:        5,
	}
	createTxLookupBlockChain := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, _ string) (*BlockChain, error) {
		return createBlockChain(db, cacheConf, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, createTxLookupBlockChain)
		})
	}
}

func TestTxLookupSkipIndexingBlockChain(t *testing.T) {
	cacheConf := &CacheConfig{
		TrieCleanLimit:            256,
		TrieDirtyLimit:            256,
		TrieDirtyCommitTarget:     20,
		TriePrefetcherParallelism: 4,
		Pruning:                   true,
		CommitInterval:            4096,
		StateHistory:              32,
		SnapshotLimit:             256,
		SnapshotNoBuild:           true, // Ensure the test errors if snapshot initialization fails
		AcceptorQueueLimit:        64,   // ensure channel doesn't block
		TransactionHistory:        5,
		SkipTxIndexing:            true,
	}
	createTxLookupBlockChain := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash, _ string) (*BlockChain, error) {
		return createBlockChain(db, cacheConf, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, createTxLookupBlockChain)
		})
	}
}

func TestCreateThenDeletePreByzantium(t *testing.T) {
	// We want to use pre-byzantium rules where we have intermediate state roots
	// between transactions.
	config := *params.TestLaunchConfig
	config.ByzantiumBlock = nil
	config.ConstantinopleBlock = nil
	config.PetersburgBlock = nil
	config.IstanbulBlock = nil
	config.MuirGlacierBlock = nil
	config.BerlinBlock = nil
	config.LondonBlock = nil

	testCreateThenDelete(t, &config)
}
func TestCreateThenDeletePostByzantium(t *testing.T) {
	testCreateThenDelete(t, params.TestChainConfig)
}

// testCreateThenDelete tests a creation and subsequent deletion of a contract, happening
// within the same block.
func testCreateThenDelete(t *testing.T, config *params.ChainConfig) {
	var (
		engine = dummy.NewFaker()
		// A sender who makes transactions, has some funds
		key, _      = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		address     = crypto.PubkeyToAddress(key.PublicKey)
		destAddress = crypto.CreateAddress(address, 0)
		funds       = big.NewInt(params.Ether) // Note: additional funds are provided here compared to go-ethereum so test completes.
	)

	// runtime code is 	0x60ffff : PUSH1 0xFF SELFDESTRUCT, a.k.a SELFDESTRUCT(0xFF)
	code := append([]byte{0x60, 0xff, 0xff}, make([]byte, 32-3)...)
	initCode := []byte{
		// SSTORE 1:1
		byte(vm.PUSH1), 0x1,
		byte(vm.PUSH1), 0x1,
		byte(vm.SSTORE),
		// Get the runtime-code on the stack
		byte(vm.PUSH32)}
	initCode = append(initCode, code...)
	initCode = append(initCode, []byte{
		byte(vm.PUSH1), 0x0, // offset
		byte(vm.MSTORE),
		byte(vm.PUSH1), 0x3, // size
		byte(vm.PUSH1), 0x0, // offset
		byte(vm.RETURN), // return 3 bytes of zero-code
	}...)
	gspec := &Genesis{
		Config: config,
		Alloc: types.GenesisAlloc{
			address: {Balance: funds},
		},
	}
	nonce := uint64(0)
	signer := types.HomesteadSigner{}
	_, blocks, _, _ := GenerateChainWithGenesis(gspec, engine, 2, 10, func(i int, b *BlockGen) {
		fee := big.NewInt(1)
		if b.header.BaseFee != nil {
			fee = b.header.BaseFee
		}
		b.SetCoinbase(common.Address{1})
		tx, _ := types.SignNewTx(key, signer, &types.LegacyTx{
			Nonce:    nonce,
			GasPrice: new(big.Int).Set(fee),
			Gas:      100000,
			Data:     initCode,
		})
		nonce++
		b.AddTx(tx)
		tx, _ = types.SignNewTx(key, signer, &types.LegacyTx{
			Nonce:    nonce,
			GasPrice: new(big.Int).Set(fee),
			Gas:      100000,
			To:       &destAddress,
		})
		b.AddTx(tx)
		nonce++
	})
	// Import the canonical chain
	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), DefaultCacheConfig, gspec, engine, vm.Config{
		//Debug:  true,
		//Tracer: logger.NewJSONLogger(nil, os.Stdout),
	}, common.Hash{}, false)
	if err != nil {
		t.Fatalf("failed to create tester chain: %v", err)
	}
	defer chain.Stop()
	// Import the blocks
	for _, block := range blocks {
		if _, err := chain.InsertChain([]*types.Block{block}); err != nil {
			t.Fatalf("block %d: failed to insert into chain: %v", block.NumberU64(), err)
		}
	}
}

func TestDeleteThenCreate(t *testing.T) {
	var (
		engine      = dummy.NewFaker()
		key, _      = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		address     = crypto.PubkeyToAddress(key.PublicKey)
		factoryAddr = crypto.CreateAddress(address, 0)
		funds       = big.NewInt(params.Ether) // Note: additional funds are provided here compared to go-ethereum so test completes.
	)
	/*
		contract Factory {
		  function deploy(bytes memory code) public {
			address addr;
			assembly {
			  addr := create2(0, add(code, 0x20), mload(code), 0)
			  if iszero(extcodesize(addr)) {
				revert(0, 0)
			  }
			}
		  }
		}
	*/
	factoryBIN := common.Hex2Bytes("608060405234801561001057600080fd5b50610241806100206000396000f3fe608060405234801561001057600080fd5b506004361061002a5760003560e01c80627743601461002f575b600080fd5b610049600480360381019061004491906100d8565b61004b565b005b6000808251602084016000f59050803b61006457600080fd5b5050565b600061007b61007684610146565b610121565b905082815260208101848484011115610097576100966101eb565b5b6100a2848285610177565b509392505050565b600082601f8301126100bf576100be6101e6565b5b81356100cf848260208601610068565b91505092915050565b6000602082840312156100ee576100ed6101f5565b5b600082013567ffffffffffffffff81111561010c5761010b6101f0565b5b610118848285016100aa565b91505092915050565b600061012b61013c565b90506101378282610186565b919050565b6000604051905090565b600067ffffffffffffffff821115610161576101606101b7565b5b61016a826101fa565b9050602081019050919050565b82818337600083830152505050565b61018f826101fa565b810181811067ffffffffffffffff821117156101ae576101ad6101b7565b5b80604052505050565b7f4e487b7100000000000000000000000000000000000000000000000000000000600052604160045260246000fd5b600080fd5b600080fd5b600080fd5b600080fd5b6000601f19601f830116905091905056fea2646970667358221220ea8b35ed310d03b6b3deef166941140b4d9e90ea2c92f6b41eb441daf49a59c364736f6c63430008070033")

	/*
		contract C {
			uint256 value;
			constructor() {
				value = 100;
			}
			function destruct() public payable {
				selfdestruct(payable(msg.sender));
			}
			receive() payable external {}
		}
	*/
	contractABI := common.Hex2Bytes("6080604052348015600f57600080fd5b5060646000819055506081806100266000396000f3fe608060405260043610601f5760003560e01c80632b68b9c614602a576025565b36602557005b600080fd5b60306032565b005b3373ffffffffffffffffffffffffffffffffffffffff16fffea2646970667358221220ab749f5ed1fcb87bda03a74d476af3f074bba24d57cb5a355e8162062ad9a4e664736f6c63430008070033")
	contractAddr := crypto.CreateAddress2(factoryAddr, [32]byte{}, crypto.Keccak256(contractABI))

	gspec := &Genesis{
		Config: params.TestChainConfig,
		Alloc: types.GenesisAlloc{
			address: {Balance: funds},
		},
	}
	nonce := uint64(0)
	signer := types.HomesteadSigner{}
	_, blocks, _, err := GenerateChainWithGenesis(gspec, engine, 2, 10, func(i int, b *BlockGen) {
		fee := big.NewInt(1)
		if b.header.BaseFee != nil {
			fee = b.header.BaseFee
		}
		b.SetCoinbase(common.Address{1})

		// Block 1
		if i == 0 {
			tx, _ := types.SignNewTx(key, signer, &types.LegacyTx{
				Nonce:    nonce,
				GasPrice: new(big.Int).Set(fee),
				Gas:      500000,
				Data:     factoryBIN,
			})
			nonce++
			b.AddTx(tx)

			data := common.Hex2Bytes("00774360000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000a76080604052348015600f57600080fd5b5060646000819055506081806100266000396000f3fe608060405260043610601f5760003560e01c80632b68b9c614602a576025565b36602557005b600080fd5b60306032565b005b3373ffffffffffffffffffffffffffffffffffffffff16fffea2646970667358221220ab749f5ed1fcb87bda03a74d476af3f074bba24d57cb5a355e8162062ad9a4e664736f6c6343000807003300000000000000000000000000000000000000000000000000")
			tx, _ = types.SignNewTx(key, signer, &types.LegacyTx{
				Nonce:    nonce,
				GasPrice: new(big.Int).Set(fee),
				Gas:      500000,
				To:       &factoryAddr,
				Data:     data,
			})
			b.AddTx(tx)
			nonce++
		} else {
			// Block 2
			tx, _ := types.SignNewTx(key, signer, &types.LegacyTx{
				Nonce:    nonce,
				GasPrice: new(big.Int).Set(fee),
				Gas:      500000,
				To:       &contractAddr,
				Data:     common.Hex2Bytes("2b68b9c6"), // destruct
			})
			nonce++
			b.AddTx(tx)

			data := common.Hex2Bytes("00774360000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000a76080604052348015600f57600080fd5b5060646000819055506081806100266000396000f3fe608060405260043610601f5760003560e01c80632b68b9c614602a576025565b36602557005b600080fd5b60306032565b005b3373ffffffffffffffffffffffffffffffffffffffff16fffea2646970667358221220ab749f5ed1fcb87bda03a74d476af3f074bba24d57cb5a355e8162062ad9a4e664736f6c6343000807003300000000000000000000000000000000000000000000000000")
			tx, _ = types.SignNewTx(key, signer, &types.LegacyTx{
				Nonce:    nonce,
				GasPrice: new(big.Int).Set(fee),
				Gas:      500000,
				To:       &factoryAddr, // re-creation
				Data:     data,
			})
			b.AddTx(tx)
			nonce++
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	// Import the canonical chain
	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), DefaultCacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
	if err != nil {
		t.Fatalf("failed to create tester chain: %v", err)
	}
	defer chain.Stop()
	for _, block := range blocks {
		if _, err := chain.InsertChain([]*types.Block{block}); err != nil {
			t.Fatalf("block %d: failed to insert into chain: %v", block.NumberU64(), err)
		}
	}
}

// TestTransientStorageReset ensures the transient storage is wiped correctly
// between transactions.
func TestTransientStorageReset(t *testing.T) {
	var (
		engine      = dummy.NewFaker()
		key, _      = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		address     = crypto.PubkeyToAddress(key.PublicKey)
		destAddress = crypto.CreateAddress(address, 0)
		funds       = big.NewInt(params.Ether) // Note: additional funds are provided here compared to go-ethereum so test completes.
		vmConfig    = vm.Config{
			ExtraEips: []int{1153}, // Enable transient storage EIP
		}
	)
	code := append([]byte{
		// TLoad value with location 1
		byte(vm.PUSH1), 0x1,
		byte(vm.TLOAD),

		// PUSH location
		byte(vm.PUSH1), 0x1,

		// SStore location:value
		byte(vm.SSTORE),
	}, make([]byte, 32-6)...)
	initCode := []byte{
		// TSTORE 1:1
		byte(vm.PUSH1), 0x1,
		byte(vm.PUSH1), 0x1,
		byte(vm.TSTORE),

		// Get the runtime-code on the stack
		byte(vm.PUSH32)}
	initCode = append(initCode, code...)
	initCode = append(initCode, []byte{
		byte(vm.PUSH1), 0x0, // offset
		byte(vm.MSTORE),
		byte(vm.PUSH1), 0x6, // size
		byte(vm.PUSH1), 0x0, // offset
		byte(vm.RETURN), // return 6 bytes of zero-code
	}...)
	gspec := &Genesis{
		Config: params.TestChainConfig,
		Alloc: types.GenesisAlloc{
			address: {Balance: funds},
		},
	}
	nonce := uint64(0)
	signer := types.HomesteadSigner{}
	_, blocks, _, _ := GenerateChainWithGenesis(gspec, engine, 1, 10, func(i int, b *BlockGen) {
		fee := big.NewInt(1)
		if b.header.BaseFee != nil {
			fee = b.header.BaseFee
		}
		b.SetCoinbase(common.Address{1})
		tx, _ := types.SignNewTx(key, signer, &types.LegacyTx{
			Nonce:    nonce,
			GasPrice: new(big.Int).Set(fee),
			Gas:      100000,
			Data:     initCode,
		})
		nonce++
		b.AddTxWithVMConfig(tx, vmConfig)

		tx, _ = types.SignNewTx(key, signer, &types.LegacyTx{
			Nonce:    nonce,
			GasPrice: new(big.Int).Set(fee),
			Gas:      100000,
			To:       &destAddress,
		})
		b.AddTxWithVMConfig(tx, vmConfig)
		nonce++
	})

	// Initialize the blockchain with 1153 enabled.
	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), DefaultCacheConfig, gspec, engine, vmConfig, common.Hash{}, false)
	if err != nil {
		t.Fatalf("failed to create tester chain: %v", err)
	}
	defer chain.Stop()
	// Import the blocks
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert into chain: %v", err)
	}
	// Check the storage
	state, err := chain.StateAt(chain.CurrentHeader().Root)
	if err != nil {
		t.Fatalf("Failed to load state %v", err)
	}
	loc := common.BytesToHash([]byte{1})
	slot := state.GetState(destAddress, loc)
	if slot != (common.Hash{}) {
		t.Fatalf("Unexpected dirty storage slot")
	}
}

func TestEIP3651(t *testing.T) {
	var (
		aa     = common.HexToAddress("0x000000000000000000000000000000000000aaaa")
		bb     = common.HexToAddress("0x000000000000000000000000000000000000bbbb")
		engine = dummy.NewCoinbaseFaker()

		// A sender who makes transactions, has some funds
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		funds   = new(big.Int).Mul(common.Big1, big.NewInt(params.Ether))
		gspec   = &Genesis{
			Config:    params.TestChainConfig,
			Timestamp: uint64(upgrade.InitiallyActiveTime.Unix()),
			Alloc: types.GenesisAlloc{
				addr1: {Balance: funds},
				addr2: {Balance: funds},
				// The address 0xAAAA sloads 0x00 and 0x01
				aa: {
					Code: []byte{
						byte(vm.PC),
						byte(vm.PC),
						byte(vm.SLOAD),
						byte(vm.SLOAD),
					},
					Nonce:   0,
					Balance: big.NewInt(0),
				},
				// The address 0xBBBB calls 0xAAAA
				bb: {
					Code: []byte{
						byte(vm.PUSH1), 0, // out size
						byte(vm.DUP1),  // out offset
						byte(vm.DUP1),  // out insize
						byte(vm.DUP1),  // in offset
						byte(vm.PUSH2), // address
						byte(0xaa),
						byte(0xaa),
						byte(vm.GAS), // gas
						byte(vm.DELEGATECALL),
					},
					Nonce:   0,
					Balance: big.NewInt(0),
				},
			},
		}
	)

	signer := types.LatestSigner(gspec.Config)

	_, blocks, _, _ := GenerateChainWithGenesis(gspec, engine, 1, 10, func(i int, b *BlockGen) {
		b.SetCoinbase(aa)
		// One transaction to Coinbase
		txdata := &types.DynamicFeeTx{
			ChainID:    gspec.Config.ChainID,
			Nonce:      0,
			To:         &bb,
			Gas:        500000,
			GasFeeCap:  newGwei(225),
			GasTipCap:  big.NewInt(2),
			AccessList: nil,
			Data:       []byte{},
		}
		tx := types.NewTx(txdata)
		tx, _ = types.SignTx(tx, signer, key1)

		b.AddTx(tx)
	})
	chain, err := NewBlockChain(rawdb.NewMemoryDatabase(), DefaultCacheConfig, gspec, engine, vm.Config{Tracer: logger.NewMarkdownLogger(&logger.Config{}, os.Stderr)}, common.Hash{}, false)
	if err != nil {
		t.Fatalf("failed to create tester chain: %v", err)
	}
	defer chain.Stop()
	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("block %d: failed to insert into chain: %v", n, err)
	}

	block := chain.GetBlockByNumber(1)

	// 1+2: Ensure EIP-1559 access lists are accounted for via gas usage.
	innerGas := vm.GasQuickStep*2 + ethparams.ColdSloadCostEIP2929*2
	expectedGas := ethparams.TxGas + 5*vm.GasFastestStep + vm.GasQuickStep + 100 + innerGas // 100 because 0xaaaa is in access list
	if block.GasUsed() != expectedGas {
		t.Fatalf("incorrect amount of gas spent: expected %d, got %d", expectedGas, block.GasUsed())
	}

	state, _ := chain.State()

	// 3: Ensure that miner received the gasUsed * (block baseFee + effectiveGasTip).
	// Note this differs from go-ethereum where the miner receives the gasUsed * block baseFee,
	// as our handling of the coinbase payment is different.
	// Note we use block.GasUsed() here as there is only one tx.
	actual := state.GetBalance(block.Coinbase()).ToBig()
	tx := block.Transactions()[0]
	gasPrice := new(big.Int).Add(block.BaseFee(), tx.EffectiveGasTipValue(block.BaseFee()))
	expected := new(big.Int).SetUint64(block.GasUsed() * gasPrice.Uint64())
	if actual.Cmp(expected) != 0 {
		t.Fatalf("miner balance incorrect: expected %d, got %d", expected, actual)
	}

	// 4: Ensure the tx sender paid for the gasUsed * (block baseFee + effectiveGasTip).
	// Note this differs from go-ethereum where the miner receives the gasUsed * block baseFee,
	// as our handling of the coinbase payment is different.
	actual = new(big.Int).Sub(funds, state.GetBalance(addr1).ToBig())
	if actual.Cmp(expected) != 0 {
		t.Fatalf("sender balance incorrect: expected %d, got %d", expected, actual)
	}
}
