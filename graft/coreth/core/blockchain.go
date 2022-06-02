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

// Package core implements the Ethereum consensus protocol.
package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ava-labs/coreth/consensus"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/core/vm"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	lru "github.com/hashicorp/golang-lru"
)

var (
	acceptorQueueGauge = metrics.NewRegisteredGauge("blockchain/acceptor/queue/size", nil)

	ErrRefuseToCorruptArchiver = errors.New("node has operated with pruning disabled, shutting down to prevent missing tries")

	errFutureBlockUnsupported  = errors.New("future block insertion not supported")
	errCacheConfigNotSpecified = errors.New("must specify cache config")
)

const (
	bodyCacheLimit     = 256
	blockCacheLimit    = 256
	receiptsCacheLimit = 32
	txLookupCacheLimit = 1024
	badBlockLimit      = 10
	TriesInMemory      = 128

	// BlockChainVersion ensures that an incompatible database forces a resync from scratch.
	//
	// Changelog:
	//
	// - Version 4
	//   The following incompatible database changes were added:
	//   * the `BlockNumber`, `TxHash`, `TxIndex`, `BlockHash` and `Index` fields of log are deleted
	//   * the `Bloom` field of receipt is deleted
	//   * the `BlockIndex` and `TxIndex` fields of txlookup are deleted
	// - Version 5
	//  The following incompatible database changes were added:
	//    * the `TxHash`, `GasCost`, and `ContractAddress` fields are no longer stored for a receipt
	//    * the `TxHash`, `GasCost`, and `ContractAddress` fields are computed by looking up the
	//      receipts' corresponding block
	// - Version 6
	//  The following incompatible database changes were added:
	//    * Transaction lookup information stores the corresponding block number instead of block hash
	// - Version 7
	//  The following incompatible database changes were added:
	//    * Use freezer as the ancient database to maintain all ancient data
	// - Version 8
	//  The following incompatible database changes were added:
	//    * New scheme for contract code in order to separate the codes and trie nodes
	BlockChainVersion uint64 = 8

	// statsReportLimit is the time limit during import and export after which we
	// always print out progress. This avoids the user wondering what's going on.
	statsReportLimit = 8 * time.Second
)

// CacheConfig contains the configuration values for the trie caching/pruning
// that's resident in a blockchain.
type CacheConfig struct {
	TrieCleanLimit                  int     // Memory allowance (MB) to use for caching trie nodes in memory
	TrieDirtyLimit                  int     // Memory limit (MB) at which to block on insert and force a flush of dirty trie nodes to disk
	TrieDirtyCommitTarget           int     // Memory limit (MB) to target for the dirties cache before invoking commit
	CommitInterval                  uint64  // Commit the trie every [CommitInterval] blocks.
	Pruning                         bool    // Whether to disable trie write caching and GC altogether (archive node)
	AcceptorQueueLimit              int     // Blocks to queue before blocking during acceptance
	PopulateMissingTries            *uint64 // If non-nil, sets the starting height for re-generating historical tries.
	PopulateMissingTriesParallelism int     // Is the number of readers to use when trying to populate missing tries.
	AllowMissingTries               bool    // Whether to allow an archive node to run with pruning enabled
	SnapshotDelayInit               bool    // Whether to initialize snapshots on startup or wait for external call
	SnapshotLimit                   int     // Memory allowance (MB) to use for caching snapshot entries in memory
	SnapshotAsync                   bool    // Generate snapshot tree async
	SnapshotVerify                  bool    // Verify generated snapshots
	SkipSnapshotRebuild             bool    // Whether to skip rebuilding the snapshot in favor of returning an error (only set to true for tests)
	Preimages                       bool    // Whether to store preimage of trie key to the disk
}

var DefaultCacheConfig = &CacheConfig{
	TrieCleanLimit:        256,
	TrieDirtyLimit:        256,
	TrieDirtyCommitTarget: 20, // 20% overhead in memory counting (this targets 16 MB)
	Pruning:               true,
	CommitInterval:        4096,
	AcceptorQueueLimit:    64, // Provides 2 minutes of buffer (2s block target) for a commit delay
	SnapshotLimit:         256,
}

// BlockChain represents the canonical chain given a database with a genesis
// block. The Blockchain manages chain imports, reverts, chain reorganisations.
//
// Importing blocks in to the block chain happens according to the set of rules
// defined by the two stage Validator. Processing of blocks is done using the
// Processor which processes the included transaction. The validation of the state
// is done in the second part of the Validator. Failing results in aborting of
// the import.
//
// The BlockChain also helps in returning blocks from **any** chain included
// in the database as well as blocks that represents the canonical chain. It's
// important to note that GetBlock can return any block and does not need to be
// included in the canonical one where as GetBlockByNumber always represents the
// canonical chain.
type BlockChain struct {
	chainConfig *params.ChainConfig // Chain & network configuration
	cacheConfig *CacheConfig        // Cache configuration for pruning

	db ethdb.Database // Low level persistent database to store final content in

	snaps *snapshot.Tree // Snapshot tree for fast trie leaf access

	hc                *HeaderChain
	rmLogsFeed        event.Feed
	chainFeed         event.Feed
	chainSideFeed     event.Feed
	chainHeadFeed     event.Feed
	chainAcceptedFeed event.Feed
	logsFeed          event.Feed
	logsAcceptedFeed  event.Feed
	blockProcFeed     event.Feed
	txAcceptedFeed    event.Feed
	scope             event.SubscriptionScope
	genesisBlock      *types.Block

	// This mutex synchronizes chain write operations.
	// Readers don't need to take it, they can just read the database.
	chainmu sync.RWMutex

	currentBlock atomic.Value // Current head of the block chain

	stateCache    state.Database // State database to reuse between imports (contains state cache)
	stateManager  TrieWriter
	bodyCache     *lru.Cache // Cache for the most recent block bodies
	receiptsCache *lru.Cache // Cache for the most recent receipts per block
	blockCache    *lru.Cache // Cache for the most recent entire blocks
	txLookupCache *lru.Cache // Cache for the most recent transaction lookup data.

	running int32 // 0 if chain is running, 1 when stopped

	engine     consensus.Engine
	validator  Validator  // Block and state validator interface
	prefetcher Prefetcher // Block state prefetcher interface
	processor  Processor  // Block transaction processor interface
	vmConfig   vm.Config

	badBlocks *lru.Cache // Bad block cache

	lastAccepted *types.Block // Prevents reorgs past this height

	senderCacher *TxSenderCacher

	// [acceptorQueue] is a processing queue for the Acceptor. This is
	// different than [chainAcceptedFeed], which is sent an event after an accepted
	// block is processed (after each loop of the accepted worker). If there is a
	// clean shutdown, all items inserted into the [acceptorQueue] will be processed.
	acceptorQueue chan *types.Block

	// [acceptorClosingLock], and [acceptorClosed] are used
	// to synchronize the closing of the [acceptorQueue] channel.
	//
	// Because we can't check if a channel is closed without reading from it
	// (which we don't want to do as we may remove a processing block), we need
	// to use a second variable to ensure we don't close a closed channel.
	acceptorClosingLock sync.RWMutex
	acceptorClosed      bool

	// [acceptorWg] is used to wait for the acceptorQueue to clear. This is used
	// during shutdown and in tests.
	acceptorWg sync.WaitGroup

	// [acceptorTip] is the last block processed by the acceptor. This is
	// returned as the LastAcceptedBlock() to ensure clients get only fully
	// processed blocks. This may be equal to [lastAccepted].
	acceptorTip     *types.Block
	acceptorTipLock sync.Mutex
}

// NewBlockChain returns a fully initialised block chain using information
// available in the database. It initialises the default Ethereum Validator and
// Processor.
func NewBlockChain(
	db ethdb.Database, cacheConfig *CacheConfig, chainConfig *params.ChainConfig, engine consensus.Engine,
	vmConfig vm.Config, lastAcceptedHash common.Hash,
) (*BlockChain, error) {
	if cacheConfig == nil {
		return nil, errCacheConfigNotSpecified
	}
	bodyCache, _ := lru.New(bodyCacheLimit)
	receiptsCache, _ := lru.New(receiptsCacheLimit)
	blockCache, _ := lru.New(blockCacheLimit)
	txLookupCache, _ := lru.New(txLookupCacheLimit)
	badBlocks, _ := lru.New(badBlockLimit)

	bc := &BlockChain{
		chainConfig: chainConfig,
		cacheConfig: cacheConfig,
		db:          db,
		stateCache: state.NewDatabaseWithConfig(db, &trie.Config{
			Cache:     cacheConfig.TrieCleanLimit,
			Preimages: cacheConfig.Preimages,
		}),
		bodyCache:     bodyCache,
		receiptsCache: receiptsCache,
		blockCache:    blockCache,
		txLookupCache: txLookupCache,
		engine:        engine,
		vmConfig:      vmConfig,
		badBlocks:     badBlocks,
		senderCacher:  newTxSenderCacher(runtime.NumCPU()),
		acceptorQueue: make(chan *types.Block, cacheConfig.AcceptorQueueLimit),
	}
	bc.validator = NewBlockValidator(chainConfig, bc, engine)
	bc.prefetcher = newStatePrefetcher(chainConfig, bc, engine)
	bc.processor = NewStateProcessor(chainConfig, bc, engine)

	var err error
	bc.hc, err = NewHeaderChain(db, chainConfig, engine)
	if err != nil {
		return nil, err
	}
	bc.genesisBlock = bc.GetBlockByNumber(0)
	if bc.genesisBlock == nil {
		return nil, ErrNoGenesis
	}

	var nilBlock *types.Block
	bc.currentBlock.Store(nilBlock)

	// Create the state manager
	bc.stateManager = NewTrieWriter(bc.stateCache.TrieDB(), cacheConfig)

	// Re-generate current block state if it is missing
	if err := bc.loadLastState(lastAcceptedHash); err != nil {
		return nil, err
	}

	// After loading the last state (and reprocessing if necessary), we are
	// guaranteed that [acceptorTip] is equal to [lastAccepted].
	//
	// It is critical to update this vaue before performing any state repairs so
	// that all accepted blocks can be considered.
	bc.acceptorTip = bc.lastAccepted

	// Make sure the state associated with the block is available
	head := bc.CurrentBlock()
	if !bc.HasState(head.Root()) {
		return nil, fmt.Errorf("head state missing %d:%s", head.Number(), head.Hash())
	}

	if err := bc.protectTrieIndex(); err != nil {
		return nil, err
	}

	// Populate missing tries if required
	if err := bc.populateMissingTries(); err != nil {
		return nil, fmt.Errorf("could not populate missing tries: %v", err)
	}

	// If snapshot initialization is delayed for fast sync, skip initializing it here.
	// This assumes that no blocks will be processed until ResetState is called to initialize
	// the state of fast sync.
	if !bc.cacheConfig.SnapshotDelayInit {
		// Load any existing snapshot, regenerating it if loading failed (if not
		// already initialized in recovery)
		bc.initSnapshot(head)
	}

	// Start processing accepted blocks effects in the background
	go bc.startAcceptor()

	return bc, nil
}

// writeBlockAcceptedIndices writes any indices that must be persisted for accepted block.
// This includes the following:
// - transaction lookup indices
// - updating the acceptor tip index
func (bc *BlockChain) writeBlockAcceptedIndices(b *types.Block) error {
	batch := bc.db.NewBatch()
	rawdb.WriteTxLookupEntriesByBlock(batch, b)
	if err := rawdb.WriteAcceptorTip(batch, b.Hash()); err != nil {
		return fmt.Errorf("%w: failed to write acceptor tip key", err)
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("%w: failed to write tx lookup entries batch", err)
	}
	return nil
}

// flattenSnapshot attempts to flatten a block of [hash] to disk.
func (bc *BlockChain) flattenSnapshot(postAbortWork func() error, hash common.Hash) error {
	// If snapshots are not initialized, perform [postAbortWork] immediately.
	if bc.snaps == nil {
		return postAbortWork()
	}

	// Abort snapshot generation before pruning anything from trie database
	// (could occur in AcceptTrie)
	bc.snaps.AbortGeneration()

	// Perform work after snapshot generation is aborted (typically trie updates)
	if err := postAbortWork(); err != nil {
		return err
	}

	// Flatten the entire snap Trie to disk
	//
	// Note: This resumes snapshot generation.
	return bc.snaps.Flatten(hash)
}

// startAcceptor starts processing items on the [acceptorQueue]. If a [nil]
// object is placed on the [acceptorQueue], the [startAcceptor] will exit.
func (bc *BlockChain) startAcceptor() {
	log.Info("Starting Acceptor", "queue length", bc.cacheConfig.AcceptorQueueLimit)

	for next := range bc.acceptorQueue {
		acceptorQueueGauge.Dec(1)

		if err := bc.flattenSnapshot(func() error {
			return bc.stateManager.AcceptTrie(next)
		}, next.Hash()); err != nil {
			log.Crit("unable to flatten snapshot from acceptor", "blockHash", next.Hash(), "err", err)
		}

		// Update last processed and transaction lookup index
		if err := bc.writeBlockAcceptedIndices(next); err != nil {
			log.Crit("failed to write accepted block effects", "err", err)
		}

		// Fetch block logs
		logs := bc.gatherBlockLogs(next.Hash(), next.NumberU64(), false)

		// Update accepted feeds
		bc.chainAcceptedFeed.Send(ChainEvent{Block: next, Hash: next.Hash(), Logs: logs})
		if len(logs) > 0 {
			bc.logsAcceptedFeed.Send(logs)
		}
		if len(next.Transactions()) != 0 {
			bc.txAcceptedFeed.Send(NewTxsEvent{next.Transactions()})
		}

		bc.acceptorTipLock.Lock()
		bc.acceptorTip = next
		bc.acceptorTipLock.Unlock()
		bc.acceptorWg.Done()
	}
}

// addAcceptorQueue adds a new *types.Block to the [acceptorQueue]. This will
// block if there are [AcceptorQueueLimit] items in [acceptorQueue].
func (bc *BlockChain) addAcceptorQueue(b *types.Block) {
	// We only acquire a read lock here because it is ok to add items to the
	// [acceptorQueue] concurrently.
	bc.acceptorClosingLock.RLock()
	defer bc.acceptorClosingLock.RUnlock()

	if bc.acceptorClosed {
		return
	}

	acceptorQueueGauge.Inc(1)
	bc.acceptorWg.Add(1)
	bc.acceptorQueue <- b
}

// DrainAcceptorQueue blocks until all items in [acceptorQueue] have been
// processed.
func (bc *BlockChain) DrainAcceptorQueue() {
	bc.acceptorClosingLock.Lock()
	defer bc.acceptorClosingLock.Unlock()

	if bc.acceptorClosed {
		return
	}

	bc.acceptorWg.Wait()
}

// stopAcceptor sends a signal to the Acceptor to stop processing accepted
// blocks. The Acceptor will exit once all items in [acceptorQueue] have been
// processed.
func (bc *BlockChain) stopAcceptor() {
	bc.acceptorClosingLock.Lock()
	defer bc.acceptorClosingLock.Unlock()

	// If [acceptorClosed] is already false, we should just return here instead
	// of attempting to close [acceptorQueue] more than once (will cause
	// a panic).
	//
	// This typically happens when a test calls [stopAcceptor] directly (prior to
	// shutdown) and then [stopAcceptor] is called again in shutdown.
	if bc.acceptorClosed {
		return
	}

	// Although nothing should be added to [acceptorQueue] after
	// [acceptorClosed] is updated, we close the channel so the Acceptor
	// goroutine exits.
	bc.acceptorWg.Wait()
	bc.acceptorClosed = true
	close(bc.acceptorQueue)
}

func (bc *BlockChain) InitializeSnapshots() {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	head := bc.CurrentBlock()
	bc.initSnapshot(head)
}

// SenderCacher returns the *TxSenderCacher used within the core package.
func (bc *BlockChain) SenderCacher() *TxSenderCacher {
	return bc.senderCacher
}

// loadLastState loads the last known chain state from the database. This method
// assumes that the chain manager mutex is held.
func (bc *BlockChain) loadLastState(lastAcceptedHash common.Hash) error {
	// Initialize genesis state
	if lastAcceptedHash == (common.Hash{}) {
		return bc.loadGenesisState()
	}

	// Restore the last known head block
	head := rawdb.ReadHeadBlockHash(bc.db)
	if head == (common.Hash{}) {
		return errors.New("could not read head block hash")
	}
	// Make sure the entire head block is available
	currentBlock := bc.GetBlockByHash(head)
	if currentBlock == nil {
		return fmt.Errorf("could not load head block %s", head.Hex())
	}
	// Everything seems to be fine, set as the head block
	bc.currentBlock.Store(currentBlock)

	// Restore the last known head header
	currentHeader := currentBlock.Header()
	if head := rawdb.ReadHeadHeaderHash(bc.db); head != (common.Hash{}) {
		if header := bc.GetHeaderByHash(head); header != nil {
			currentHeader = header
		}
	}
	bc.hc.SetCurrentHeader(currentHeader)

	log.Info("Loaded most recent local header", "number", currentHeader.Number, "hash", currentHeader.Hash(), "age", common.PrettyAge(time.Unix(int64(currentHeader.Time), 0)))
	log.Info("Loaded most recent local full block", "number", currentBlock.Number(), "hash", currentBlock.Hash(), "age", common.PrettyAge(time.Unix(int64(currentBlock.Time()), 0)))

	// Otherwise, set the last accepted block and perform a re-org.
	bc.lastAccepted = bc.GetBlockByHash(lastAcceptedHash)
	if bc.lastAccepted == nil {
		return fmt.Errorf("could not load last accepted block")
	}

	// This ensures that the head block is updated to the last accepted block on startup
	if err := bc.setPreference(bc.lastAccepted); err != nil {
		return fmt.Errorf("failed to set preference to last accepted block while loading last state: %w", err)
	}

	// reprocessState is necessary to ensure that the last accepted state is
	// available. The state may not be available if it was not committed due
	// to an unclean shutdown.
	return bc.reprocessState(bc.lastAccepted, 2*bc.cacheConfig.CommitInterval)
}

func (bc *BlockChain) loadGenesisState() error {
	// Prepare the genesis block and reinitialise the chain
	batch := bc.db.NewBatch()
	rawdb.WriteBlock(batch, bc.genesisBlock)
	if err := batch.Write(); err != nil {
		log.Crit("Failed to write genesis block", "err", err)
	}
	bc.writeHeadBlock(bc.genesisBlock)

	// Last update all in-memory chain markers
	bc.lastAccepted = bc.genesisBlock
	bc.currentBlock.Store(bc.genesisBlock)
	bc.hc.SetGenesis(bc.genesisBlock.Header())
	bc.hc.SetCurrentHeader(bc.genesisBlock.Header())
	return nil
}

// Export writes the active chain to the given writer.
func (bc *BlockChain) Export(w io.Writer) error {
	return bc.ExportN(w, uint64(0), bc.CurrentBlock().NumberU64())
}

// ExportN writes a subset of the active chain to the given writer.
func (bc *BlockChain) ExportN(w io.Writer, first uint64, last uint64) error {
	bc.chainmu.RLock()
	defer bc.chainmu.RUnlock()

	if first > last {
		return fmt.Errorf("export failed: first (%d) is greater than last (%d)", first, last)
	}
	log.Info("Exporting batch of blocks", "count", last-first+1)

	start, reported := time.Now(), time.Now()
	for nr := first; nr <= last; nr++ {
		block := bc.GetBlockByNumber(nr)
		if block == nil {
			return fmt.Errorf("export failed on #%d: not found", nr)
		}
		if err := block.EncodeRLP(w); err != nil {
			return err
		}
		if time.Since(reported) >= statsReportLimit {
			log.Info("Exporting blocks", "exported", block.NumberU64()-first, "elapsed", common.PrettyDuration(time.Since(start)))
			reported = time.Now()
		}
	}
	return nil
}

// writeHeadBlock injects a new head block into the current block chain. This method
// assumes that the block is indeed a true head. It will also reset the head
// header to this very same block if they are older or if they are on a different side chain.
//
// Note, this function assumes that the `mu` mutex is held!
func (bc *BlockChain) writeHeadBlock(block *types.Block) {
	// If the block is on a side chain or an unknown one, force other heads onto it too
	// Add the block to the canonical chain number scheme and mark as the head
	batch := bc.db.NewBatch()
	rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())

	rawdb.WriteHeadBlockHash(batch, block.Hash())
	rawdb.WriteHeadHeaderHash(batch, block.Hash())

	// Flush the whole batch into the disk, exit the node if failed
	if err := batch.Write(); err != nil {
		log.Crit("Failed to update chain indexes and markers", "err", err)
	}
	// Update all in-memory chain markers in the last step
	bc.hc.SetCurrentHeader(block.Header())
	bc.currentBlock.Store(block)
}

// ValidateCanonicalChain confirms a canonical chain is well-formed.
func (bc *BlockChain) ValidateCanonicalChain() error {
	// Ensure all accepted blocks are fully processed
	bc.DrainAcceptorQueue()

	current := bc.CurrentBlock()
	i := 0
	log.Info("Beginning to validate canonical chain", "startBlock", current.NumberU64())

	for current.Hash() != bc.genesisBlock.Hash() {
		blkByHash := bc.GetBlockByHash(current.Hash())
		if blkByHash == nil {
			return fmt.Errorf("couldn't find block by hash %s at height %d", current.Hash().String(), current.Number())
		}
		if blkByHash.Hash() != current.Hash() {
			return fmt.Errorf("blockByHash returned a block with an unexpected hash: %s, expected: %s", blkByHash.Hash().String(), current.Hash().String())
		}
		blkByNumber := bc.GetBlockByNumber(current.Number().Uint64())
		if blkByNumber == nil {
			return fmt.Errorf("couldn't find block by number at height %d", current.Number())
		}
		if blkByNumber.Hash() != current.Hash() {
			return fmt.Errorf("blockByNumber returned a block with unexpected hash: %s, expected: %s", blkByNumber.Hash().String(), current.Hash().String())
		}

		hdrByHash := bc.GetHeaderByHash(current.Hash())
		if hdrByHash == nil {
			return fmt.Errorf("couldn't find block header by hash %s at height %d", current.Hash().String(), current.Number())
		}
		if hdrByHash.Hash() != current.Hash() {
			return fmt.Errorf("hdrByHash returned a block header with an unexpected hash: %s, expected: %s", hdrByHash.Hash().String(), current.Hash().String())
		}
		hdrByNumber := bc.GetHeaderByNumber(current.Number().Uint64())
		if hdrByNumber == nil {
			return fmt.Errorf("couldn't find block header by number at height %d", current.Number())
		}
		if hdrByNumber.Hash() != current.Hash() {
			return fmt.Errorf("hdrByNumber returned a block header with unexpected hash: %s, expected: %s", hdrByNumber.Hash().String(), current.Hash().String())
		}

		txs := current.Body().Transactions

		// Transactions are only indexed beneath the last accepted block, so we only check
		// that the transactions have been indexed, if we are checking below the last accepted
		// block.
		if current.NumberU64() <= bc.lastAccepted.NumberU64() {
			// Ensure that all of the transactions have been stored correctly in the canonical
			// chain
			for txIndex, tx := range txs {
				txLookup := bc.GetTransactionLookup(tx.Hash())
				if txLookup == nil {
					return fmt.Errorf("failed to find transaction %s", tx.Hash().String())
				}
				if txLookup.BlockHash != current.Hash() {
					return fmt.Errorf("tx lookup returned with incorrect block hash: %s, expected: %s", txLookup.BlockHash.String(), current.Hash().String())
				}
				if txLookup.BlockIndex != current.Number().Uint64() {
					return fmt.Errorf("tx lookup returned with incorrect block index: %d, expected: %d", txLookup.BlockIndex, current.Number().Uint64())
				}
				if txLookup.Index != uint64(txIndex) {
					return fmt.Errorf("tx lookup returned with incorrect transaction index: %d, expected: %d", txLookup.Index, txIndex)
				}
			}
		}

		blkReceipts := bc.GetReceiptsByHash(current.Hash())
		if blkReceipts.Len() != len(txs) {
			return fmt.Errorf("found %d transaction receipts, expected %d", blkReceipts.Len(), len(txs))
		}
		for index, txReceipt := range blkReceipts {
			if txReceipt.TxHash != txs[index].Hash() {
				return fmt.Errorf("transaction receipt mismatch, expected %s, but found: %s", txs[index].Hash().String(), txReceipt.TxHash.String())
			}
			if txReceipt.BlockHash != current.Hash() {
				return fmt.Errorf("transaction receipt had block hash %s, but expected %s", txReceipt.BlockHash.String(), current.Hash().String())
			}
			if txReceipt.BlockNumber.Uint64() != current.NumberU64() {
				return fmt.Errorf("transaction receipt had block number %d, but expected %d", txReceipt.BlockNumber.Uint64(), current.NumberU64())
			}
		}

		i += 1
		if i%1000 == 0 {
			log.Info("Validate Canonical Chain Update", "totalBlocks", i)
		}

		parent := bc.GetBlockByHash(current.ParentHash())
		if parent.Hash() != current.ParentHash() {
			return fmt.Errorf("getBlockByHash retrieved parent block with incorrect hash, found %s, expected: %s", parent.Hash().String(), current.ParentHash().String())
		}
		current = parent
	}

	return nil
}

// Stop stops the blockchain service. If any imports are currently in progress
// it will abort them using the procInterrupt.
func (bc *BlockChain) Stop() {
	if !atomic.CompareAndSwapInt32(&bc.running, 0, 1) {
		return
	}

	// Wait for accepted feed to process all remaining items
	log.Info("Stopping Acceptor")
	start := time.Now()
	bc.stopAcceptor()
	log.Info("Acceptor queue drained", "t", time.Since(start))

	log.Info("Shutting down state manager")
	start = time.Now()
	if err := bc.stateManager.Shutdown(); err != nil {
		log.Error("Failed to Shutdown state manager", "err", err)
	}
	log.Info("State manager shut down", "t", time.Since(start))

	// Stop senderCacher's goroutines
	log.Info("Shutting down sender cacher")
	bc.senderCacher.Shutdown()

	// Unsubscribe all subscriptions registered from blockchain.
	log.Info("Closing scope")
	bc.scope.Close()

	log.Info("Blockchain stopped")
}

// SetPreference attempts to update the head block to be the provided block and
// emits a ChainHeadEvent if successful. This function will handle all reorg
// side effects, if necessary.
//
// Note: This function should ONLY be called on blocks that have already been
// inserted into the chain.
//
// Assumes [bc.chainmu] is not held by the caller.
func (bc *BlockChain) SetPreference(block *types.Block) error {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	return bc.setPreference(block)
}

// setPreference attempts to update the head block to be the provided block and
// emits a ChainHeadEvent if successful. This function will handle all reorg
// side effects, if necessary.
//
// Assumes [bc.chainmu] is held by the caller.
func (bc *BlockChain) setPreference(block *types.Block) error {
	current := bc.CurrentBlock()

	// Return early if the current block is already the block
	// we are trying to write.
	if current.Hash() == block.Hash() {
		return nil
	}

	log.Debug("Setting preference", "number", block.Number(), "hash", block.Hash())

	// writeKnownBlock updates the head block and will handle any reorg side
	// effects automatically.
	if err := bc.writeKnownBlock(block); err != nil {
		return fmt.Errorf("unable to invoke writeKnownBlock: %w", err)
	}

	// Send a ChainHeadEvent if we end up altering
	// the head block. Many internal aysnc processes rely on
	// receiving these events (i.e. the TxPool).
	bc.chainHeadFeed.Send(ChainHeadEvent{Block: block})
	return nil
}

// LastAcceptedBlock returns the last block to be marked as accepted. It may or
// may not yet be processed.
func (bc *BlockChain) LastConsensusAcceptedBlock() *types.Block {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	return bc.lastAccepted
}

// LastAcceptedBlock returns the last block to be marked as accepted and is
// processed.
//
// Note: During initialization, [acceptorTip] is equal to [lastAccepted].
func (bc *BlockChain) LastAcceptedBlock() *types.Block {
	bc.acceptorTipLock.Lock()
	defer bc.acceptorTipLock.Unlock()

	return bc.acceptorTip
}

// Accept sets a minimum height at which no reorg can pass. Additionally,
// this function may trigger a reorg if the block being accepted is not in the
// canonical chain.
//
// Assumes [bc.chainmu] is not held by the caller.
func (bc *BlockChain) Accept(block *types.Block) error {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	// The parent of [block] must be the last accepted block.
	if bc.lastAccepted.Hash() != block.ParentHash() {
		return fmt.Errorf(
			"expected accepted block to have parent %s:%d but got %s:%d",
			bc.lastAccepted.Hash().Hex(),
			bc.lastAccepted.NumberU64(),
			block.ParentHash().Hex(),
			block.NumberU64()-1,
		)
	}

	// If the canonical hash at the block height does not match the block we are
	// accepting, we need to trigger a reorg.
	canonical := bc.GetCanonicalHash(block.NumberU64())
	if canonical != block.Hash() {
		log.Debug("Accepting block in non-canonical chain", "number", block.Number(), "hash", block.Hash())
		if err := bc.setPreference(block); err != nil {
			return fmt.Errorf("could not set new preferred block %d:%s as preferred: %w", block.Number(), block.Hash(), err)
		}
	}

	bc.lastAccepted = block
	bc.addAcceptorQueue(block)
	return nil
}

func (bc *BlockChain) Reject(block *types.Block) error {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	// Reject Trie
	if err := bc.stateManager.RejectTrie(block); err != nil {
		return fmt.Errorf("unable to reject trie: %w", err)
	}

	if bc.snaps != nil {
		if err := bc.snaps.Discard(block.Hash()); err != nil {
			log.Error("unable to discard snap from rejected block", "block", block.Hash(), "number", block.NumberU64(), "root", block.Root())
		}
	}

	// Remove the block since its data is no longer needed
	batch := bc.db.NewBatch()
	rawdb.DeleteBlock(batch, block.Hash(), block.NumberU64())
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write delete block batch: %w", err)
	}

	return nil
}

// writeKnownBlock updates the head block flag with a known block
// and introduces chain reorg if necessary.
func (bc *BlockChain) writeKnownBlock(block *types.Block) error {
	current := bc.CurrentBlock()
	if block.ParentHash() != current.Hash() {
		if err := bc.reorg(current, block); err != nil {
			return err
		}
	}
	bc.writeHeadBlock(block)
	return nil
}

// writeCanonicalBlockWithLogs writes the new head [block] and emits events
// for the new head block.
func (bc *BlockChain) writeCanonicalBlockWithLogs(block *types.Block, logs []*types.Log) {
	bc.writeHeadBlock(block)
	bc.chainFeed.Send(ChainEvent{Block: block, Hash: block.Hash(), Logs: logs})
	if len(logs) > 0 {
		bc.logsFeed.Send(logs)
	}
	bc.chainHeadFeed.Send(ChainHeadEvent{Block: block})
}

// newTip returns a boolean indicating if the block should be appended to
// the canonical chain.
func (bc *BlockChain) newTip(block *types.Block) bool {
	return block.ParentHash() == bc.CurrentBlock().Hash()
}

// writeBlockAndSetHead persists the block and associated state to the database
// and optimistically updates the canonical chain if [block] extends the current
// canonical chain.
// writeBlockAndSetHead expects to be the last verification step during InsertBlock
// since it creates a reference that will only be cleaned up by Accept/Reject.
func (bc *BlockChain) writeBlockAndSetHead(block *types.Block, receipts []*types.Receipt, logs []*types.Log, state *state.StateDB) error {
	if err := bc.writeBlockWithState(block, receipts, logs, state); err != nil {
		return err
	}

	// If [block] represents a new tip of the canonical chain, we optimistically add it before
	// setPreference is called. Otherwise, we consider it a side chain block.
	if bc.newTip(block) {
		bc.writeCanonicalBlockWithLogs(block, logs)
	} else {
		bc.chainSideFeed.Send(ChainSideEvent{Block: block})
	}

	return nil
}

// writeBlockWithState writes the block and all associated state to the database,
// but it expects the chain mutex to be held.
func (bc *BlockChain) writeBlockWithState(block *types.Block, receipts []*types.Receipt, logs []*types.Log, state *state.StateDB) error {
	// Irrelevant of the canonical status, write the block itself to the database.
	//
	// Note all the components of block(hash->number map, header, body, receipts)
	// should be written atomically. BlockBatch is used for containing all components.
	blockBatch := bc.db.NewBatch()
	rawdb.WriteBlock(blockBatch, block)
	rawdb.WriteReceipts(blockBatch, block.Hash(), block.NumberU64(), receipts)
	rawdb.WritePreimages(blockBatch, state.Preimages())
	if err := blockBatch.Write(); err != nil {
		log.Crit("Failed to write block into disk", "err", err)
	}
	// Commit all cached state changes into underlying memory database.
	// If snapshots are enabled, call CommitWithSnaps to explicitly create a snapshot
	// diff layer for the block.
	var err error
	if bc.snaps == nil {
		_, err = state.Commit(bc.chainConfig.IsEIP158(block.Number()))
	} else {
		_, err = state.CommitWithSnap(bc.chainConfig.IsEIP158(block.Number()), bc.snaps, block.Hash(), block.ParentHash())
	}
	if err != nil {
		return err
	}

	// Note: if InsertTrie must be the last step in verification that can return an error.
	// This allows [stateManager] to assume that if it inserts a trie without returning an
	// error then the block has passed verification and either AcceptTrie/RejectTrie will
	// eventually be called on [root] unless a fatal error occurs. It does not assume that
	// the node will not shutdown before either AcceptTrie/RejectTrie is called.
	if err := bc.stateManager.InsertTrie(block); err != nil {
		if bc.snaps != nil {
			discardErr := bc.snaps.Discard(block.Hash())
			if discardErr != nil {
				log.Debug("failed to discard snapshot after being unable to insert block trie", "block", block.Hash(), "root", block.Root())
			}
		}
		return err
	}

	return nil
}

// InsertChain attempts to insert the given batch of blocks in to the canonical
// chain or, otherwise, create a fork. If an error is returned it will return
// the index number of the failing block as well an error describing what went
// wrong.
//
// After insertion is done, all accumulated events will be fired.
func (bc *BlockChain) InsertChain(chain types.Blocks) (int, error) {
	// Sanity check that we have something meaningful to import
	if len(chain) == 0 {
		return 0, nil
	}

	bc.blockProcFeed.Send(true)
	defer bc.blockProcFeed.Send(false)

	// Do a sanity check that the provided chain is actually ordered and linked.
	for i := 1; i < len(chain); i++ {
		block, prev := chain[i], chain[i-1]
		if block.NumberU64() != prev.NumberU64()+1 || block.ParentHash() != prev.Hash() {
			log.Error("Non contiguous block insert",
				"number", block.Number(),
				"hash", block.Hash(),
				"parent", block.ParentHash(),
				"prevnumber", prev.Number(),
				"prevhash", prev.Hash(),
			)
			return 0, fmt.Errorf("non contiguous insert: item %d is #%d [%x…], item %d is #%d [%x…] (parent [%x…])", i-1, prev.NumberU64(),
				prev.Hash().Bytes()[:4], i, block.NumberU64(), block.Hash().Bytes()[:4], block.ParentHash().Bytes()[:4])
		}
	}
	// Pre-checks passed, start the full block imports
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()
	for n, block := range chain {
		if err := bc.insertBlock(block, true); err != nil {
			return n, err
		}
	}

	return len(chain), nil
}

func (bc *BlockChain) InsertBlock(block *types.Block) error {
	return bc.InsertBlockManual(block, true)
}

func (bc *BlockChain) InsertBlockManual(block *types.Block, writes bool) error {
	bc.blockProcFeed.Send(true)
	defer bc.blockProcFeed.Send(false)

	bc.chainmu.Lock()
	err := bc.insertBlock(block, writes)
	bc.chainmu.Unlock()

	return err
}

// gatherBlockLogs fetches logs from a previously inserted block.
func (bc *BlockChain) gatherBlockLogs(hash common.Hash, number uint64, removed bool) []*types.Log {
	receipts := rawdb.ReadReceipts(bc.db, hash, number, bc.chainConfig)
	var logs []*types.Log
	for _, receipt := range receipts {
		for _, log := range receipt.Logs {
			l := *log
			if removed {
				l.Removed = true
			}
			logs = append(logs, &l)
		}
	}

	return logs
}

func (bc *BlockChain) insertBlock(block *types.Block, writes bool) error {
	bc.senderCacher.Recover(types.MakeSigner(bc.chainConfig, block.Number(), new(big.Int).SetUint64(block.Time())), block.Transactions())

	err := bc.engine.VerifyHeader(bc, block.Header())
	if err == nil {
		err = bc.validator.ValidateBody(block)
	}

	switch {
	case errors.Is(err, ErrKnownBlock):
		// even if the block is already known, we still need to generate the
		// snapshot layer and add a reference to the triedb, so we re-execute
		// the block. Note that insertBlock should only be called on a block
		// once if it returns nil
		if bc.newTip(block) {
			log.Debug("Setting head to be known block", "number", block.Number(), "hash", block.Hash())
		} else {
			log.Debug("Reprocessing already known block", "number", block.Number(), "hash", block.Hash())
		}

	// If an ancestor has been pruned, then this block cannot be acceptable.
	case errors.Is(err, consensus.ErrPrunedAncestor):
		return errors.New("side chain insertion is not supported")

	// Future blocks are not supported, but should not be reported, so we return an error
	// early here
	case errors.Is(err, consensus.ErrFutureBlock):
		return errFutureBlockUnsupported

	// Some other error occurred, abort
	case err != nil:
		bc.reportBlock(block, nil, err)
		return err
	}
	// No validation errors for the block
	var activeState *state.StateDB
	defer func() {
		// The chain importer is starting and stopping trie prefetchers. If a bad
		// block or other error is hit however, an early return may not properly
		// terminate the background threads. This defer ensures that we clean up
		// and dangling prefetcher, without defering each and holding on live refs.
		if activeState != nil {
			activeState.StopPrefetcher()
		}
	}()

	// Retrieve the parent block and its state to execute on top
	start := time.Now()

	// Retrieve the parent block and its state to execute block
	parent := bc.GetHeader(block.ParentHash(), block.NumberU64()-1)
	statedb, err := state.New(parent.Root, bc.stateCache, bc.snaps)
	if err != nil {
		return err
	}

	// Enable prefetching to pull in trie node paths while processing transactions
	statedb.StartPrefetcher("chain")
	activeState = statedb

	// If we have a followup block, run that against the current state to pre-cache
	// transactions and probabilistically some of the account/storage trie nodes.
	// Process block using the parent state as reference point
	receipts, logs, usedGas, err := bc.processor.Process(block, parent, statedb, bc.vmConfig)
	if err != nil {
		bc.reportBlock(block, receipts, err)
		return err
	}

	// Validate the state using the default validator
	if err := bc.validator.ValidateState(block, statedb, receipts, usedGas); err != nil {
		bc.reportBlock(block, receipts, err)
		return err
	}

	// If [writes] are disabled, skip [writeBlockWithState] so that we do not write the block
	// or the state trie to disk.
	// Note: in pruning mode, this prevents us from generating a reference to the state root.
	if !writes {
		return nil
	}

	// Write the block to the chain and get the status.
	// writeBlockWithState (called within writeBlockAndSethead) creates a reference that
	// will be cleaned up in Accept/Reject so we need to ensure an error cannot occur
	// later in verification, since that would cause the referenced root to never be dereferenced.
	if err := bc.writeBlockAndSetHead(block, receipts, logs, statedb); err != nil {
		return err
	}
	log.Debug("Inserted new block", "number", block.Number(), "hash", block.Hash(),
		"parentHash", block.ParentHash(),
		"uncles", len(block.Uncles()), "txs", len(block.Transactions()), "gas", block.GasUsed(),
		"elapsed", common.PrettyDuration(time.Since(start)),
		"root", block.Root(), "baseFeePerGas", block.BaseFee(), "blockGasCost", block.BlockGasCost(),
	)

	return nil
}

// collectLogs collects the logs that were generated or removed during
// the processing of the block that corresponds with the given hash.
// These logs are later announced as deleted or reborn.
func (bc *BlockChain) collectLogs(hash common.Hash, removed bool) []*types.Log {
	number := bc.hc.GetBlockNumber(hash)
	if number == nil {
		return nil
	}
	return bc.gatherBlockLogs(hash, *number, removed)
}

// mergeLogs returns a merged log slice with specified sort order.
func mergeLogs(logs [][]*types.Log, reverse bool) []*types.Log {
	var ret []*types.Log
	if reverse {
		for i := len(logs) - 1; i >= 0; i-- {
			ret = append(ret, logs[i]...)
		}
	} else {
		for i := 0; i < len(logs); i++ {
			ret = append(ret, logs[i]...)
		}
	}
	return ret
}

// reorg takes two blocks, an old chain and a new chain and will reconstruct the
// blocks and inserts them to be part of the new canonical chain and accumulates
// potential missing transactions and post an event about them.
func (bc *BlockChain) reorg(oldBlock, newBlock *types.Block) error {
	var (
		newHead = newBlock
		oldHead = oldBlock

		newChain    types.Blocks
		oldChain    types.Blocks
		commonBlock *types.Block

		deletedLogs [][]*types.Log
		rebirthLogs [][]*types.Log
	)
	// Reduce the longer chain to the same number as the shorter one
	if oldBlock.NumberU64() > newBlock.NumberU64() {
		// Old chain is longer, gather all transactions and logs as deleted ones
		for ; oldBlock != nil && oldBlock.NumberU64() != newBlock.NumberU64(); oldBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1) {
			oldChain = append(oldChain, oldBlock)
			// Collect deleted logs for notification
			logs := bc.collectLogs(oldBlock.Hash(), true)
			if len(logs) > 0 {
				deletedLogs = append(deletedLogs, logs)
			}
		}
	} else {
		// New chain is longer, stash all blocks away for subsequent insertion
		for ; newBlock != nil && newBlock.NumberU64() != oldBlock.NumberU64(); newBlock = bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1) {
			newChain = append(newChain, newBlock)
		}
	}
	if oldBlock == nil {
		return fmt.Errorf("invalid old chain")
	}
	if newBlock == nil {
		return fmt.Errorf("invalid new chain")
	}
	// Both sides of the reorg are at the same number, reduce both until the common
	// ancestor is found
	for {
		// If the common ancestor was found, bail out
		if oldBlock.Hash() == newBlock.Hash() {
			commonBlock = oldBlock
			break
		}
		// Remove an old block as well as stash away a new block
		oldChain = append(oldChain, oldBlock)
		// Collect deleted logs for notification
		logs := bc.collectLogs(oldBlock.Hash(), true)
		if len(logs) > 0 {
			deletedLogs = append(deletedLogs, logs)
		}

		newChain = append(newChain, newBlock)

		// Step back with both chains
		oldBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1)
		if oldBlock == nil {
			return fmt.Errorf("invalid old chain")
		}
		newBlock = bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1)
		if newBlock == nil {
			return fmt.Errorf("invalid new chain")
		}
	}

	// If the commonBlock is less than the last accepted height, we return an error
	// because performing a reorg would mean removing an accepted block from the
	// canonical chain.
	if commonBlock.NumberU64() < bc.lastAccepted.NumberU64() {
		return fmt.Errorf("cannot orphan finalized block at height: %d to common block at height: %d", bc.lastAccepted.NumberU64(), commonBlock.NumberU64())
	}

	// Ensure the user sees large reorgs
	if len(oldChain) > 0 && len(newChain) > 0 {
		logFn := log.Info
		msg := "Resetting chain preference"
		if len(oldChain) > 63 {
			msg = "Large chain preference change detected"
			logFn = log.Warn
		}
		logFn(msg, "number", commonBlock.Number(), "hash", commonBlock.Hash(),
			"drop", len(oldChain), "dropfrom", oldChain[0].Hash(), "add", len(newChain), "addfrom", newChain[0].Hash())
	} else {
		log.Warn("Unlikely preference change (rewind to ancestor) occurred", "oldnum", oldHead.Number(), "oldhash", oldHead.Hash(), "newnum", newHead.Number(), "newhash", newHead.Hash())
	}
	// Insert the new chain(except the head block(reverse order)),
	// taking care of the proper incremental order.
	for i := len(newChain) - 1; i >= 1; i-- {
		// Insert the block in the canonical way, re-writing history
		bc.writeHeadBlock(newChain[i])

		// Collect reborn logs due to chain reorg
		logs := bc.collectLogs(newChain[i].Hash(), false)
		if len(logs) > 0 {
			rebirthLogs = append(rebirthLogs, logs)
		}
	}
	// Delete any canonical number assignments above the new head
	indexesBatch := bc.db.NewBatch()

	// Use the height of [newHead] to determine which canonical hashes to remove
	// in case the new chain is shorter than the old chain, in which case
	// there may be hashes set on the canonical chain that were invalidated
	// but not yet overwritten by the re-org.
	for i := newHead.NumberU64() + 1; ; i++ {
		hash := rawdb.ReadCanonicalHash(bc.db, i)
		if hash == (common.Hash{}) {
			break
		}
		rawdb.DeleteCanonicalHash(indexesBatch, i)
	}
	if err := indexesBatch.Write(); err != nil {
		log.Crit("Failed to delete useless indexes", "err", err)
	}
	// If any logs need to be fired, do it now. In theory we could avoid creating
	// this goroutine if there are no events to fire, but realistcally that only
	// ever happens if we're reorging empty blocks, which will only happen on idle
	// networks where performance is not an issue either way.
	if len(deletedLogs) > 0 {
		bc.rmLogsFeed.Send(RemovedLogsEvent{mergeLogs(deletedLogs, true)})
	}
	if len(rebirthLogs) > 0 {
		bc.logsFeed.Send(mergeLogs(rebirthLogs, false))
	}
	if len(oldChain) > 0 {
		for i := len(oldChain) - 1; i >= 0; i-- {
			bc.chainSideFeed.Send(ChainSideEvent{Block: oldChain[i]})
		}
	}
	return nil
}

// BadBlocks returns a list of the last 'bad blocks' that the client has seen on the network
func (bc *BlockChain) BadBlocks() []*types.Block {
	blocks := make([]*types.Block, 0, bc.badBlocks.Len())
	for _, hash := range bc.badBlocks.Keys() {
		if blk, exist := bc.badBlocks.Peek(hash); exist {
			block := blk.(*types.Block)
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// addBadBlock adds a bad block to the bad-block LRU cache
func (bc *BlockChain) addBadBlock(block *types.Block) {
	bc.badBlocks.Add(block.Hash(), block)
}

// reportBlock logs a bad block error.
func (bc *BlockChain) reportBlock(block *types.Block, receipts types.Receipts, err error) {
	bc.addBadBlock(block)

	var receiptString string
	for i, receipt := range receipts {
		receiptString += fmt.Sprintf("\t %d: cumulative: %v gas: %v contract: %v status: %v tx: %v logs: %v bloom: %x state: %x\n",
			i, receipt.CumulativeGasUsed, receipt.GasUsed, receipt.ContractAddress.Hex(),
			receipt.Status, receipt.TxHash.Hex(), receipt.Logs, receipt.Bloom, receipt.PostState)
	}
	log.Error(fmt.Sprintf(`
########## BAD BLOCK #########
Chain config: %v

Number: %v
Hash: 0x%x
%v

Error: %v
##############################
`, bc.chainConfig, block.Number(), block.Hash(), receiptString, err))
}

func (bc *BlockChain) RemoveRejectedBlocks(start, end uint64) error {
	batch := bc.db.NewBatch()

	for i := start; i < end; i++ {
		hashes := rawdb.ReadAllHashes(bc.db, i)
		canonicalBlock := bc.GetBlockByNumber((i))
		if canonicalBlock == nil {
			return fmt.Errorf("failed to retrieve block by number at height %d", i)
		}
		canonicalHash := canonicalBlock.Hash()
		for _, hash := range hashes {
			if hash == canonicalHash {
				continue
			}
			rawdb.DeleteBlock(batch, hash, i)
		}

		if err := batch.Write(); err != nil {
			return fmt.Errorf("failed to write delete rejected block batch at height %d", i)
		}
		batch.Reset()
	}

	return nil
}

// reprocessBlock reprocesses a previously accepted block. This is often used
// to regenerate previously pruned state tries.
func (bc *BlockChain) reprocessBlock(parent *types.Block, current *types.Block) (common.Hash, error) {
	// Retrieve the parent block and its state to execute block
	var (
		statedb    *state.StateDB
		err        error
		parentRoot = parent.Root()
	)
	// We don't simply use [NewWithSnapshot] here because it doesn't return an
	// error if [bc.snaps != nil] and [bc.snaps.Snapshot(parentRoot) == nil].
	if bc.snaps == nil {
		statedb, err = state.New(parentRoot, bc.stateCache, nil)
	} else {
		snap := bc.snaps.Snapshot(parentRoot)
		if snap == nil {
			return common.Hash{}, fmt.Errorf("failed to get snapshot for parent root: %s", parentRoot)
		}
		statedb, err = state.NewWithSnapshot(parentRoot, bc.stateCache, snap)
	}
	if err != nil {
		return common.Hash{}, fmt.Errorf("could not fetch state for (%s: %d): %v", parent.Hash().Hex(), parent.NumberU64(), err)
	}

	// Enable prefetching to pull in trie node paths while processing transactions
	statedb.StartPrefetcher("chain")
	defer func() {
		statedb.StopPrefetcher()
	}()

	// Process previously stored block
	receipts, _, usedGas, err := bc.processor.Process(current, parent.Header(), statedb, vm.Config{})
	if err != nil {
		return common.Hash{}, fmt.Errorf("failed to re-process block (%s: %d): %v", current.Hash().Hex(), current.NumberU64(), err)
	}

	// Validate the state using the default validator
	if err := bc.validator.ValidateState(current, statedb, receipts, usedGas); err != nil {
		return common.Hash{}, fmt.Errorf("failed to validate state while re-processing block (%s: %d): %v", current.Hash().Hex(), current.NumberU64(), err)
	}
	log.Debug("Processed block", "block", current.Hash(), "number", current.NumberU64())

	// Commit all cached state changes into underlying memory database.
	// If snapshots are enabled, call CommitWithSnaps to explicitly create a snapshot
	// diff layer for the block.
	if bc.snaps == nil {
		return statedb.Commit(bc.chainConfig.IsEIP158(current.Number()))
	}
	return statedb.CommitWithSnap(bc.chainConfig.IsEIP158(current.Number()), bc.snaps, current.Hash(), current.ParentHash())
}

// initSnapshot instantiates a Snapshot instance and adds it to [bc]
func (bc *BlockChain) initSnapshot(b *types.Block) {
	if bc.cacheConfig.SnapshotLimit <= 0 || bc.snaps != nil {
		return
	}

	// If we are starting from genesis, generate the original snapshot disk layer
	// up front, so we can use it while executing blocks in bootstrapping. This
	// also avoids a costly async generation process when reaching tip.
	//
	// Additionally, we should always repair a snapshot if starting at genesis
	// if [SnapshotLimit] > 0.
	async := bc.cacheConfig.SnapshotAsync && b.NumberU64() > 0
	rebuild := !bc.cacheConfig.SkipSnapshotRebuild || b.NumberU64() == 0
	log.Info("Initializing snapshots", "async", async, "rebuild", rebuild, "headHash", b.Hash(), "headRoot", b.Root())
	var err error
	bc.snaps, err = snapshot.New(bc.db, bc.stateCache.TrieDB(), bc.cacheConfig.SnapshotLimit, b.Hash(), b.Root(), async, rebuild, bc.cacheConfig.SnapshotVerify)
	if err != nil {
		log.Error("failed to initialize snapshots", "headHash", b.Hash(), "headRoot", b.Root(), "err", err, "async", async)
	}
}

// reprocessState reprocesses the state up to [block], iterating through its ancestors until
// it reaches a block with a state committed to the database. reprocessState does not use
// snapshots since the disk layer for snapshots will most likely be above the last committed
// state that reprocessing will start from.
func (bc *BlockChain) reprocessState(current *types.Block, reexec uint64) error {
	origin := current.NumberU64()
	acceptorTip, err := rawdb.ReadAcceptorTip(bc.db)
	if err != nil {
		return fmt.Errorf("%w: unable to get Acceptor tip", err)
	}
	log.Info("Loaded Acceptor tip", "hash", acceptorTip)

	// The acceptor tip is up to date either if it matches the current hash, or it has not been
	// initialized (i.e., this node has not accepted any blocks asynchronously).
	acceptorTipUpToDate := acceptorTip == (common.Hash{}) || acceptorTip == current.Hash()

	// If the state is already available and the acceptor tip is up to date, skip re-processing.
	if bc.HasState(current.Root()) && acceptorTipUpToDate {
		log.Info("Skipping state reprocessing", "root", current.Root())
		return nil
	}

	// If the acceptorTip is a non-empty hash, jump re-processing back to the acceptor tip to ensure that
	// we re-process at a minimum from the last processed accepted block.
	// Note: we do not have a guarantee that the last trie on disk will be at a height <= acceptorTip.
	// Since we need to re-process from at least the acceptorTip to ensure indices are updated correctly
	// we must start searching for the block to start re-processing at the acceptorTip.
	// This may occur if we are running in archive mode where every block's trie is committed on insertion
	// or during an unclean shutdown.
	if acceptorTip != (common.Hash{}) {
		current = bc.GetBlockByHash(acceptorTip)
		if current == nil {
			return fmt.Errorf("failed to get block for acceptor tip %s", acceptorTip)
		}
	}

	for i := 0; i < int(reexec); i++ {
		// TODO: handle canceled context

		if current.NumberU64() == 0 {
			return errors.New("genesis state is missing")
		}
		parent := bc.GetBlock(current.ParentHash(), current.NumberU64()-1)
		if parent == nil {
			return fmt.Errorf("missing block %s:%d", current.ParentHash().Hex(), current.NumberU64()-1)
		}
		current = parent
		_, err = bc.stateCache.OpenTrie(current.Root())
		if err == nil {
			break
		}
	}
	if err != nil {
		switch err.(type) {
		case *trie.MissingNodeError:
			return fmt.Errorf("required historical state unavailable (reexec=%d)", reexec)
		default:
			return err
		}
	}

	// State was available at historical point, regenerate
	var (
		start        = time.Now()
		logged       time.Time
		previousRoot common.Hash
		triedb       = bc.stateCache.TrieDB()
		writeIndices bool
	)
	// Note: we add 1 since in each iteration, we attempt to re-execute the next block.
	log.Info("Re-executing blocks to generate state for last accepted block", "from", current.NumberU64()+1, "to", origin)
	for current.NumberU64() < origin {
		// TODO: handle canceled context

		// Print progress logs if long enough time elapsed
		if time.Since(logged) > 8*time.Second {
			log.Info("Regenerating historical state", "block", current.NumberU64()+1, "target", origin, "remaining", origin-current.NumberU64(), "elapsed", time.Since(start))
			logged = time.Now()
		}

		// Retrieve the next block to regenerate and process it
		parent := current
		next := current.NumberU64() + 1
		if current = bc.GetBlockByNumber(next); current == nil {
			return fmt.Errorf("failed to retrieve block %d while re-generating state", next)
		}

		// Initialize snapshot if required (prevents full snapshot re-generation in
		// the case of unclean shutdown)
		if parent.Hash() == acceptorTip {
			log.Info("Recovering snapshot", "hash", parent.Hash(), "index", parent.NumberU64())
			// TODO: switch to checking the snapshot block hash markers here to ensure that when we re-process the block, we have the opportunity to apply
			// a snapshot diff layer that we may have been in the middle of committing during shutdown. This will prevent snapshot re-generation in the case
			// that the node stops mid-way through snapshot flattening (performed across multiple DB batches).
			// If snapshot initialization is delayed due to state sync, skip initializing snaps here
			if !bc.cacheConfig.SnapshotDelayInit {
				bc.initSnapshot(parent)
			}
			writeIndices = true // Set [writeIndices] to true, so that the indices will be updated from the last accepted tip onwards.
		}

		// Reprocess next block using previously fetched data
		root, err := bc.reprocessBlock(parent, current)
		if err != nil {
			return err
		}

		// Flatten snapshot if initialized, holding a reference to the state root until the next block
		// is processed.
		if err := bc.flattenSnapshot(func() error {
			triedb.Reference(root, common.Hash{})
			if previousRoot != (common.Hash{}) {
				triedb.Dereference(previousRoot)
			}
			previousRoot = root
			return nil
		}, current.Hash()); err != nil {
			return err
		}

		// Write any unsaved indices to disk
		if writeIndices {
			if err := bc.writeBlockAcceptedIndices(current); err != nil {
				return fmt.Errorf("%w: failed to process accepted block indices", err)
			}
		}
	}

	nodes, imgs := triedb.Size()
	log.Info("Historical state regenerated", "block", current.NumberU64(), "elapsed", time.Since(start), "nodes", nodes, "preimages", imgs)
	if previousRoot != (common.Hash{}) {
		return triedb.Commit(previousRoot, true, nil)
	}
	return nil
}

func (bc *BlockChain) protectTrieIndex() error {
	if !bc.cacheConfig.Pruning {
		return rawdb.WritePruningDisabled(bc.db)
	}
	pruningDisabled, err := rawdb.HasPruningDisabled(bc.db)
	if err != nil {
		return fmt.Errorf("failed to check if the chain has been run with pruning disabled: %w", err)
	}
	if !pruningDisabled {
		return nil
	}
	if !bc.cacheConfig.AllowMissingTries {
		return ErrRefuseToCorruptArchiver
	}
	return nil
}

// populateMissingTries iterates from [bc.cacheConfig.PopulateMissingTries] (defaults to 0)
// to [LastAcceptedBlock] and persists all tries to disk that are not already on disk. This is
// used to fill trie index gaps in an "archive" node without resyncing from scratch.
//
// NOTE: Assumes the genesis root and last accepted root are written to disk
func (bc *BlockChain) populateMissingTries() error {
	if bc.cacheConfig.PopulateMissingTries == nil {
		return nil
	}

	var (
		lastAccepted = bc.LastAcceptedBlock().NumberU64()
		startHeight  = *bc.cacheConfig.PopulateMissingTries
		startTime    = time.Now()
		logged       time.Time
		triedb       = bc.stateCache.TrieDB()
		missing      = 0
	)

	// Do not allow the config to specify a starting point above the last accepted block.
	if startHeight > lastAccepted {
		return fmt.Errorf("cannot populate missing tries from a starting point (%d) > last accepted block (%d)", startHeight, lastAccepted)
	}

	// If we are starting from the genesis, increment the start height by 1 so we don't attempt to re-process
	// the genesis block.
	if startHeight == 0 {
		startHeight += 1
	}
	parent := bc.GetBlockByNumber(startHeight - 1)
	if parent == nil {
		return fmt.Errorf("failed to fetch initial parent block for re-populate missing tries at height %d", startHeight-1)
	}

	it := newBlockChainIterator(bc, startHeight, bc.cacheConfig.PopulateMissingTriesParallelism)
	defer it.Stop()

	for i := startHeight; i < lastAccepted; i++ {
		// Print progress logs if long enough time elapsed
		if time.Since(logged) > 8*time.Second {
			log.Info("Populating missing tries", "missing", missing, "block", i, "remaining", lastAccepted-i, "elapsed", time.Since(startTime))
			logged = time.Now()
		}

		// TODO: handle canceled context
		current, hasState, err := it.Next(context.TODO())
		if err != nil {
			return fmt.Errorf("error while populating missing tries: %w", err)
		}

		if hasState {
			parent = current
			continue
		}

		root, err := bc.reprocessBlock(parent, current)
		if err != nil {
			return err
		}

		// Commit root to disk so that it can be accessed directly
		if err := triedb.Commit(root, false, nil); err != nil {
			return err
		}
		parent = current
		log.Debug("Populated missing trie", "block", current.NumberU64(), "root", root)
		missing++
	}

	// Write marker to DB to indicate populate missing tries finished successfully.
	// Note: writing the marker here means that we do allow consecutive runs of re-populating
	// missing tries if it does not finish during the prior run.
	if err := rawdb.WritePopulateMissingTries(bc.db); err != nil {
		return fmt.Errorf("failed to write offline pruning success marker: %w", err)
	}

	nodes, imgs := triedb.Size()
	log.Info("All missing tries populated", "startHeight", startHeight, "lastAcceptedHeight", lastAccepted, "missing", missing, "elapsed", time.Since(startTime), "nodes", nodes, "preimages", imgs)
	return nil
}

// CleanBlockRootsAboveLastAccepted gathers the blocks that may have previously been in processing above the
// last accepted block and wipes their block roots from disk to mark their tries as inaccessible.
// This is used prior to pruning to ensure that all of the tries that may still be in processing are marked
// as inaccessible and mirrors the handling of middle roots in the geth offline pruning implementation.
// This is not strictly necessary, but maintains a soft assumption.
func (bc *BlockChain) CleanBlockRootsAboveLastAccepted() error {
	targetRoot := bc.LastAcceptedBlock().Root()

	// Clean up any block roots above the last accepted block before we start pruning.
	// Note: this takes the place of middleRoots in the geth implementation since we do not
	// track processing block roots via snapshot journals in the same way.
	processingRoots := bc.gatherBlockRootsAboveLastAccepted()
	// If there is a block above the last accepted block with an identical state root, we
	// explicitly remove it from the set to ensure we do not corrupt the last accepted trie.
	delete(processingRoots, targetRoot)
	for processingRoot := range processingRoots {
		// Delete the processing root from disk to mark the trie as inaccessible (no need to handle this in a batch).
		if err := bc.db.Delete(processingRoot[:]); err != nil {
			return fmt.Errorf("failed to remove processing root (%s) preparing for offline pruning: %w", processingRoot, err)
		}
	}

	return nil
}

// gatherBlockRootsAboveLastAccepted iterates forward from the last accepted block and returns a list of all block roots
// for any blocks that were inserted above the last accepted block.
// Given that we never insert a block into the chain unless all of its ancestors have been inserted, this should gather
// all of the block roots for blocks inserted above the last accepted block that may have been in processing at some point
// in the past and are therefore potentially still acceptable.
// Note: there is an edge case where the node dies while the consensus engine is rejecting a branch of blocks since the
// consensus engine will reject the lowest ancestor first. In this case, these blocks will not be considered acceptable in
// the future.
// Ex.
//    A
//  /   \
// B     C
// |
// D
// |
// E
// |
// F
//
// The consensus engine accepts block C and proceeds to reject the other branch in order (B, D, E, F).
// If the consensus engine dies after rejecting block D, block D will be deleted, such that the forward iteration
// may not find any blocks at this height and will not reach the previously processing blocks E and F.
func (bc *BlockChain) gatherBlockRootsAboveLastAccepted() map[common.Hash]struct{} {
	blockRoots := make(map[common.Hash]struct{})
	for height := bc.lastAccepted.NumberU64() + 1; ; height++ {
		blockHashes := rawdb.ReadAllHashes(bc.db, height)
		// If there are no block hashes at [height], then there should be no further acceptable blocks
		// past this point.
		if len(blockHashes) == 0 {
			break
		}

		// Fetch the blocks and append their roots.
		for _, blockHash := range blockHashes {
			block := bc.GetBlockByHash(blockHash)
			if block == nil {
				continue
			}

			blockRoots[block.Root()] = struct{}{}
		}
	}

	return blockRoots
}

// ResetState reinitializes the state of the blockchain
// to the trie represented by [block.Root()] after updating
// in-memory current block pointers to [block].
// Only used in state sync.
func (bc *BlockChain) ResetState(block *types.Block) error {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	// Update head block and snapshot pointers on disk
	batch := bc.db.NewBatch()
	rawdb.WriteAcceptorTip(batch, block.Hash())
	rawdb.WriteHeadBlockHash(batch, block.Hash())
	rawdb.WriteHeadHeaderHash(batch, block.Hash())
	rawdb.WriteSnapshotBlockHash(batch, block.Hash())
	rawdb.WriteSnapshotRoot(batch, block.Root())
	if err := batch.Write(); err != nil {
		return err
	}

	// Update all in-memory chain markers
	bc.lastAccepted = block
	bc.acceptorTip = block
	bc.currentBlock.Store(block)
	bc.hc.SetCurrentHeader(block.Header())

	lastAcceptedHash := block.Hash()
	bc.stateCache = state.NewDatabaseWithConfig(bc.db, &trie.Config{
		Cache:     bc.cacheConfig.TrieCleanLimit,
		Preimages: bc.cacheConfig.Preimages,
	})
	if err := bc.loadLastState(lastAcceptedHash); err != nil {
		return err
	}
	// Create the state manager
	bc.stateManager = NewTrieWriter(bc.stateCache.TrieDB(), bc.cacheConfig)

	// Make sure the state associated with the block is available
	head := bc.CurrentBlock()
	if !bc.HasState(head.Root()) {
		return fmt.Errorf("head state missing %d:%s", head.Number(), head.Hash())
	}

	bc.initSnapshot(head)
	return nil
}
