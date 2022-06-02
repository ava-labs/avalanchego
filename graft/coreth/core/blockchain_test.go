// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/consensus/dummy"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/state/pruner"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	archiveConfig = &CacheConfig{
		TrieCleanLimit:        256,
		TrieDirtyLimit:        256,
		TrieDirtyCommitTarget: 20,
		Pruning:               false, // Archive mode
		SnapshotLimit:         256,
		AcceptorQueueLimit:    64,
	}

	pruningConfig = &CacheConfig{
		TrieCleanLimit:        256,
		TrieDirtyLimit:        256,
		TrieDirtyCommitTarget: 20,
		Pruning:               true, // Enable pruning
		CommitInterval:        4096,
		SnapshotLimit:         256,
		AcceptorQueueLimit:    64,
	}
)

func createBlockChain(
	db ethdb.Database,
	cacheConfig *CacheConfig,
	chainConfig *params.ChainConfig,
	lastAcceptedHash common.Hash,
) (*BlockChain, error) {
	// Import the chain. This runs all block validation rules.
	blockchain, err := NewBlockChain(
		db,
		cacheConfig,
		chainConfig,
		dummy.NewDummyEngine(&dummy.ConsensusCallbacks{
			OnExtraStateChange: func(block *types.Block, sdb *state.StateDB) (*big.Int, *big.Int, error) {
				sdb.SetBalanceMultiCoin(common.HexToAddress("0xdeadbeef"), common.HexToHash("0xdeadbeef"), big.NewInt(block.Number().Int64()))
				return nil, nil, nil
			},
			OnFinalizeAndAssemble: func(header *types.Header, sdb *state.StateDB, txs []*types.Transaction) ([]byte, *big.Int, *big.Int, error) {
				sdb.SetBalanceMultiCoin(common.HexToAddress("0xdeadbeef"), common.HexToHash("0xdeadbeef"), big.NewInt(header.Number.Int64()))
				return nil, nil, nil, nil
			},
		}),
		vm.Config{},
		lastAcceptedHash,
	)
	return blockchain, err
}

func TestArchiveBlockChain(t *testing.T) {
	createArchiveBlockChain := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		return createBlockChain(db, archiveConfig, chainConfig, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, createArchiveBlockChain)
		})
	}
}

func TestArchiveBlockChainSnapsDisabled(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		return createBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit:        256,
				TrieDirtyLimit:        256,
				TrieDirtyCommitTarget: 20,
				Pruning:               false, // Archive mode
				SnapshotLimit:         0,     // Disable snapshots
				AcceptorQueueLimit:    64,
			},
			chainConfig,
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
	createPruningBlockChain := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		return createBlockChain(db, pruningConfig, chainConfig, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, createPruningBlockChain)
		})
	}
}

func TestPruningBlockChainSnapsDisabled(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		return createBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit:        256,
				TrieDirtyLimit:        256,
				TrieDirtyCommitTarget: 20,
				Pruning:               true, // Enable pruning
				CommitInterval:        4096,
				SnapshotLimit:         0, // Disable snapshots
				AcceptorQueueLimit:    64,
			},
			chainConfig,
			lastAcceptedHash,
		)
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
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		blockchain, err := createBlockChain(db, pruningConfig, chainConfig, lastAcceptedHash)
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
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		blockchain, err := createBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit:        256,
				TrieDirtyLimit:        256,
				TrieDirtyCommitTarget: 20,
				Pruning:               true, // Enable pruning
				CommitInterval:        4096,
				SnapshotLimit:         0, // Disable snapshots
				AcceptorQueueLimit:    64,
			},
			chainConfig,
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
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := createBlockChain(
			db,
			&CacheConfig{
				TrieCleanLimit:        256,
				TrieDirtyLimit:        256,
				TrieDirtyCommitTarget: 20,
				Pruning:               true, // Enable pruning
				CommitInterval:        4096,
				SnapshotLimit:         snapLimit,
				AcceptorQueueLimit:    64,
			},
			chainConfig,
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
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Delete the snapshot block hash and state root to ensure that if we die in between writing a snapshot
		// diff layer to disk at any point, we can still recover on restart.
		rawdb.DeleteSnapshotBlockHash(db)
		rawdb.DeleteSnapshotRoot(db)

		return createBlockChain(db, pruningConfig, chainConfig, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, create)
		})
	}
}

func TestBlockChainOfflinePruningUngracefulShutdown(t *testing.T) {
	create := func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Import the chain. This runs all block validation rules.
		blockchain, err := createBlockChain(db, pruningConfig, chainConfig, lastAcceptedHash)
		if err != nil {
			return nil, err
		}

		// Overwrite state manager, so that Shutdown is not called.
		// This tests to ensure that the state manager handles an ungraceful shutdown correctly.
		blockchain.stateManager = &wrappedStateManager{TrieWriter: blockchain.stateManager}

		if lastAcceptedHash == (common.Hash{}) {
			return blockchain, nil
		}

		tempDir := t.TempDir()
		if err := blockchain.CleanBlockRootsAboveLastAccepted(); err != nil {
			return nil, err
		}
		pruner, err := pruner.NewPruner(db, tempDir, 256)
		if err != nil {
			return nil, fmt.Errorf("offline pruning failed (%s, %d): %w", tempDir, 256, err)
		}

		targetRoot := blockchain.LastAcceptedBlock().Root()
		if err := pruner.Prune(targetRoot); err != nil {
			return nil, fmt.Errorf("failed to prune blockchain with target root: %s due to: %w", targetRoot, err)
		}
		// Re-initialize the blockchain after pruning
		return createBlockChain(db, pruningConfig, chainConfig, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
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
		// We use two separate databases since GenerateChain commits the state roots to its underlying
		// database.
		genDB            = rawdb.NewMemoryDatabase()
		chainDB          = rawdb.NewMemoryDatabase()
		lastAcceptedHash common.Hash
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  GenesisAlloc{addr1: {Balance: genesisBalance}},
	}
	genesis := gspec.MustCommit(genDB)
	_ = gspec.MustCommit(chainDB)

	blockchain, err := createBlockChain(chainDB, pruningConfig, gspec.Config, lastAcceptedHash)
	if err != nil {
		t.Fatal(err)
	}
	defer blockchain.Stop()

	// This call generates a chain of 3 blocks.
	signer := types.HomesteadSigner{}
	// Generate chain of blocks using [genDB] instead of [chainDB] to avoid writing
	// to the BlockChain's database while generating blocks.
	chain, _, err := GenerateChain(gspec.Config, genesis, blockchain.engine, genDB, 10, 10, func(i int, gen *BlockGen) {
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), params.TxGas, nil, nil), signer, key1)
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

	lastAcceptedHash = blockchain.LastConsensusAcceptedBlock().Hash()
	blockchain.Stop()

	blockchain, err = createBlockChain(chainDB, pruningConfig, gspec.Config, lastAcceptedHash)
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
			Pruning:                         false, // Archive mode
			SnapshotLimit:                   256,
			PopulateMissingTries:            &startHeight, // Starting point for re-populating.
			PopulateMissingTriesParallelism: parallelism,
			AcceptorQueueLimit:              64,
		},
		gspec.Config,
		lastAcceptedHash,
	)
	if err != nil {
		t.Fatal(err)
	}

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
	var (
		create = func(db ethdb.Database, chainConfig *params.ChainConfig, lastAcceptedHash common.Hash) (*BlockChain, error) {
			blockchain, err := createBlockChain(db, &CacheConfig{
				TrieCleanLimit:        256,
				TrieDirtyLimit:        256,
				TrieDirtyCommitTarget: 20,
				Pruning:               true,
				CommitInterval:        4096,
				SnapshotLimit:         256,
				SkipSnapshotRebuild:   true, // Ensure the test errors if snapshot initialization fails
				AcceptorQueueLimit:    1000, // ensure channel doesn't block
			}, chainConfig, lastAcceptedHash)
			if err != nil {
				return nil, err
			}
			return blockchain, nil
		}

		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		// We use two separate databases since GenerateChain commits the state roots to its underlying
		// database.
		genDB   = rawdb.NewMemoryDatabase()
		chainDB = rawdb.NewMemoryDatabase()
	)

	// Ensure that key1 has some funds in the genesis block.
	genesisBalance := big.NewInt(1000000)
	gspec := &Genesis{
		Config: &params.ChainConfig{HomesteadBlock: new(big.Int)},
		Alloc:  GenesisAlloc{addr1: {Balance: genesisBalance}},
	}
	genesis := gspec.MustCommit(genDB)
	_ = gspec.MustCommit(chainDB)

	blockchain, err := create(chainDB, gspec.Config, common.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	defer blockchain.Stop()

	// This call generates a chain of 10 blocks.
	signer := types.HomesteadSigner{}
	// Generate chain of blocks using [genDB] instead of [chainDB] to avoid writing
	// to the BlockChain's database while generating blocks.
	chain, _, err := GenerateChain(gspec.Config, genesis, blockchain.engine, genDB, 10, 10, func(i int, gen *BlockGen) {
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), params.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	if err != nil {
		t.Fatal(err)
	}

	// Insert three blocks into the chain and accept only the first block.
	if _, err := blockchain.InsertChain(chain); err != nil {
		t.Fatal(err)
	}

	foundTxs := []common.Hash{}
	missingTxs := []common.Hash{}
	for i, block := range chain {
		if err := blockchain.Accept(block); err != nil {
			t.Fatal(err)
		}

		if i == 3 {
			// At height 3, kill the async accepted block processor to force an
			// ungraceful recovery
			blockchain.stopAcceptor()
			blockchain.acceptorQueue = nil
		}

		if i <= 3 {
			// If <= height 3, all txs should be accessible on lookup
			for _, tx := range block.Transactions() {
				foundTxs = append(foundTxs, tx.Hash())
			}
		} else {
			// If > 3, all txs should be accessible on lookup
			for _, tx := range block.Transactions() {
				missingTxs = append(missingTxs, tx.Hash())
			}
		}
	}

	// After inserting all blocks, we should confirm that txs added after the
	// async worker shutdown cannot be found.
	for _, tx := range foundTxs {
		txLookup := blockchain.GetTransactionLookup(tx)
		if txLookup == nil {
			t.Fatalf("missing transaction: %v", tx)
		}
	}
	for _, tx := range missingTxs {
		txLookup := blockchain.GetTransactionLookup(tx)
		if txLookup != nil {
			t.Fatalf("transaction should be missing: %v", tx)
		}
	}

	// check the state of the last accepted block
	checkState := func(sdb *state.StateDB) error {
		nonce := sdb.GetNonce(addr1)
		if nonce != 10 {
			return fmt.Errorf("expected nonce addr1: 10, found nonce: %d", nonce)
		}
		transferredFunds := big.NewInt(100000)
		balance1 := sdb.GetBalance(addr1)
		expectedBalance1 := new(big.Int).Sub(genesisBalance, transferredFunds)
		if balance1.Cmp(expectedBalance1) != 0 {
			return fmt.Errorf("expected addr1 balance: %d, found balance: %d", expectedBalance1, balance1)
		}

		balance2 := sdb.GetBalance(addr2)
		expectedBalance2 := transferredFunds
		if balance2.Cmp(expectedBalance2) != 0 {
			return fmt.Errorf("expected addr2 balance: %d, found balance: %d", expectedBalance2, balance2)
		}

		nonce = sdb.GetNonce(addr2)
		if nonce != 0 {
			return fmt.Errorf("expected addr2 nonce: 0, found nonce: %d", nonce)
		}
		return nil
	}

	_, newChain, restartedChain := checkBlockChainState(t, blockchain, gspec, chainDB, create, checkState)

	allTxs := append(foundTxs, missingTxs...)
	for _, bc := range []*BlockChain{newChain, restartedChain} {
		// We should confirm that snapshots were properly initialized
		if bc.snaps == nil {
			t.Fatal("snapshot initialization failed")
		}

		// We should confirm all transactions can now be queried
		for _, tx := range allTxs {
			txLookup := bc.GetTransactionLookup(tx)
			if txLookup == nil {
				t.Fatalf("missing transaction: %v", tx)
			}
		}
	}
}
