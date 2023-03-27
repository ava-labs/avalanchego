// (c) 2020-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package core

import (
	"fmt"
	"math/big"
	"os"
	"testing"
	"time"

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
	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	gspec *Genesis,
	lastAcceptedHash common.Hash,
) (*BlockChain, error) {
	// Import the chain. This runs all block validation rules.
	blockchain, err := NewBlockChain(
		db,
		cacheConfig,
		gspec,
		dummy.NewDummyEngine(&TestCallbacks),
		vm.Config{},
		lastAcceptedHash,
		false,
	)
	return blockchain, err
}

func TestArchiveBlockChain(t *testing.T) {
	createArchiveBlockChain := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
		return createBlockChain(db, archiveConfig, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, createArchiveBlockChain)
		})
	}
}

// awaitWatcherEventsSubside waits for at least one event on [watcher] and then waits
// for at least [subsideTimeout] before returning
func awaitWatcherEventsSubside(watcher *fsnotify.Watcher, subsideTimeout time.Duration) {
	done := make(chan struct{})

	go func() {
		defer func() {
			close(done)
		}()

		select {
		case <-watcher.Events:
		case <-watcher.Errors:
			return
		}

		for {
			select {
			case <-watcher.Events:
			case <-watcher.Errors:
				return
			case <-time.After(subsideTimeout):
				return
			}
		}
	}()
	<-done
}

func TestTrieCleanJournal(t *testing.T) {
	if os.Getenv("RUN_FLAKY_TESTS") != "true" {
		t.Skip("FLAKY")
	}
	require := require.New(t)
	assert := assert.New(t)

	trieCleanJournal := t.TempDir()
	trieCleanJournalWatcher, err := fsnotify.NewWatcher()
	require.NoError(err)
	defer func() {
		assert.NoError(trieCleanJournalWatcher.Close())
	}()
	require.NoError(trieCleanJournalWatcher.Add(trieCleanJournal))

	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
		config := *archiveConfig
		config.TrieCleanJournal = trieCleanJournal
		config.TrieCleanRejournal = 100 * time.Millisecond
		return createBlockChain(db, &config, gspec, lastAcceptedHash)
	}

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
		Alloc:  GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{})
	require.NoError(err)
	defer blockchain.Stop()

	// This call generates a chain of 3 blocks.
	signer := types.HomesteadSigner{}
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 3, 10, func(i int, gen *BlockGen) {
		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(10000), params.TxGas, nil, nil), signer, key1)
		gen.AddTx(tx)
	})
	require.NoError(err)

	// Insert and accept the generated chain
	_, err = blockchain.InsertChain(chain)
	require.NoError(err)

	for _, block := range chain {
		require.NoError(blockchain.Accept(block))
	}
	blockchain.DrainAcceptorQueue()

	awaitWatcherEventsSubside(trieCleanJournalWatcher, time.Second)
	// Assert that a new file is created in the trie clean journal
	dirEntries, err := os.ReadDir(trieCleanJournal)
	require.NoError(err)
	require.NotEmpty(dirEntries)
}

func TestArchiveBlockChainSnapsDisabled(t *testing.T) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
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
	createPruningBlockChain := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
		return createBlockChain(db, pruningConfig, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, createPruningBlockChain)
		})
	}
}

func TestPruningBlockChainSnapsDisabled(t *testing.T) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
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

type wrappedStateManager struct {
	TrieWriter
}

func (w *wrappedStateManager) Shutdown() error { return nil }

func TestPruningBlockChainUngracefulShutdown(t *testing.T) {
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
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
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
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
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
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
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
		// Delete the snapshot block hash and state root to ensure that if we die in between writing a snapshot
		// diff layer to disk at any point, we can still recover on restart.
		rawdb.DeleteSnapshotBlockHash(db)
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
	create := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
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
		return createBlockChain(db, pruningConfig, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt := tt
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
		Alloc:  GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := createBlockChain(chainDB, pruningConfig, gspec, common.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	defer blockchain.Stop()

	// This call generates a chain of 3 blocks.
	signer := types.HomesteadSigner{}
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 10, 10, func(i int, gen *BlockGen) {
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
		create = func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
			blockchain, err := createBlockChain(db, &CacheConfig{
				TrieCleanLimit:        256,
				TrieDirtyLimit:        256,
				TrieDirtyCommitTarget: 20,
				Pruning:               true,
				CommitInterval:        4096,
				SnapshotLimit:         256,
				SkipSnapshotRebuild:   true, // Ensure the test errors if snapshot initialization fails
				AcceptorQueueLimit:    1000, // ensure channel doesn't block
			}, gspec, lastAcceptedHash)
			if err != nil {
				return nil, err
			}
			return blockchain, nil
		}

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
		Alloc:  GenesisAlloc{addr1: {Balance: genesisBalance}},
	}

	blockchain, err := create(chainDB, gspec, common.Hash{})
	if err != nil {
		t.Fatal(err)
	}
	defer blockchain.Stop()

	// This call generates a chain of 10 blocks.
	signer := types.HomesteadSigner{}
	_, chain, _, err := GenerateChainWithGenesis(gspec, blockchain.engine, 10, 10, func(i int, gen *BlockGen) {
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

// TestCanonicalHashMarker tests all the canonical hash markers are updated/deleted
// correctly in case reorg is called.
func TestCanonicalHashMarker(t *testing.T) {
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
				Alloc:   GenesisAlloc{},
				BaseFee: big.NewInt(params.ApricotPhase3InitialBaseFee),
			}
			engine = dummy.NewFaker()
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
		diskdb := rawdb.NewMemoryDatabase()
		chain, err := NewBlockChain(diskdb, DefaultCacheConfig, gspec, engine, vm.Config{}, common.Hash{}, false)
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
	}
}

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
	height := uint64(128)
	genDb, blocks, _, err := GenerateChainWithGenesis(gspec, dummy.NewDummyEngine(&TestCallbacks), int(height), 10, func(i int, block *BlockGen) {
		tx, err := types.SignTx(types.NewTransaction(block.TxNonce(addr1), addr2, big.NewInt(10000), params.TxGas, nil, nil), signer, key1)
		require.NoError(err)
		block.AddTx(tx)
	})
	require.NoError(err)

	blocks2, _, err := GenerateChain(gspec.Config, blocks[len(blocks)-1], dummy.NewDummyEngine(&TestCallbacks), genDb, 10, 10, nil)
	require.NoError(err)

	check := func(tail *uint64, chain *BlockChain) {
		stored := rawdb.ReadTxIndexTail(chain.db)
		require.EqualValues(tail, stored)

		if tail == nil {
			return
		}
		for i := *tail; i <= chain.CurrentBlock().NumberU64(); i++ {
			block := rawdb.ReadBlock(chain.db, rawdb.ReadCanonicalHash(chain.db, i), i)
			if block.Transactions().Len() == 0 {
				continue
			}
			for _, tx := range block.Transactions() {
				index := rawdb.ReadTxLookupEntry(chain.db, tx.Hash())
				require.NotNilf(index, "Miss transaction indices, number %d hash %s", i, tx.Hash().Hex())
			}
		}

		for i := uint64(0); i < *tail; i++ {
			block := rawdb.ReadBlock(chain.db, rawdb.ReadCanonicalHash(chain.db, i), i)
			if block.Transactions().Len() == 0 {
				continue
			}
			for _, tx := range block.Transactions() {
				index := rawdb.ReadTxLookupEntry(chain.db, tx.Hash())
				require.Nilf(index, "Transaction indices should be deleted, number %d hash %s", i, tx.Hash().Hex())
			}
		}
	}

	conf := &CacheConfig{
		TrieCleanLimit:        256,
		TrieDirtyLimit:        256,
		TrieDirtyCommitTarget: 20,
		Pruning:               true,
		CommitInterval:        4096,
		SnapshotLimit:         256,
		SkipSnapshotRebuild:   true, // Ensure the test errors if snapshot initialization fails
		AcceptorQueueLimit:    64,
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

	chain.Stop()
	check(nil, chain) // check all indices has been indexed

	lastAcceptedHash := chain.CurrentHeader().Hash()

	// Reconstruct a block chain which only reserves limited tx indices
	// 128 blocks were previously indexed. Now we add a new block at each test step.
	limit := []uint64{130 /* 129 + 1 reserve all */, 64 /* drop stale */, 32 /* shorten history */}
	tails := []uint64{0 /* reserve all */, 67 /* 130 - 64 + 1 */, 100 /* 131 - 32 + 1 */}
	for i, l := range limit {
		conf.TxLookupLimit = l

		chain, err := createBlockChain(chainDB, conf, gspec, lastAcceptedHash)
		require.NoError(err)

		newBlks := blocks2[i : i+1]
		_, err = chain.InsertChain(newBlks) // Feed chain a higher block to trigger indices updater.
		require.NoError(err)

		err = chain.Accept(newBlks[0]) // Accept the block to trigger indices updater.
		require.NoError(err)

		chain.DrainAcceptorQueue()
		time.Sleep(50 * time.Millisecond) // Wait for indices initialisation

		chain.Stop()
		check(&tails[i], chain)

		lastAcceptedHash = chain.CurrentHeader().Hash()
	}
}

func TestTxLookupBlockChain(t *testing.T) {
	cacheConf := &CacheConfig{
		TrieCleanLimit:        256,
		TrieDirtyLimit:        256,
		TrieDirtyCommitTarget: 20,
		Pruning:               true,
		CommitInterval:        4096,
		SnapshotLimit:         256,
		SkipSnapshotRebuild:   true, // Ensure the test errors if snapshot initialization fails
		AcceptorQueueLimit:    64,   // ensure channel doesn't block
		TxLookupLimit:         5,
	}
	createTxLookupBlockChain := func(db ethdb.Database, gspec *Genesis, lastAcceptedHash common.Hash) (*BlockChain, error) {
		return createBlockChain(db, cacheConf, gspec, lastAcceptedHash)
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			tt.testFunc(t, createTxLookupBlockChain)
		})
	}
}
