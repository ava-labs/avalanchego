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
	"errors"
	"fmt"
	"io"
	"math/big"
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
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	lru "github.com/hashicorp/golang-lru"
)

var (
	removeTxIndicesKey = []byte("removed_tx_indices")

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
	TrieCleanLimit int  // Memory allowance (MB) to use for caching trie nodes in memory
	TrieDirtyLimit int  // Memory limit (MB) at which to start flushing dirty trie nodes to disk
	Pruning        bool // Whether to disable trie write caching and GC altogether (archive node)
	SnapshotLimit  int  // Memory allowance (MB) to use for caching snapshot entries in memory
	SnapshotAsync  bool // Generate snapshot tree async
	SnapshotVerify bool // Verify generated snapshots
	Preimages      bool // Whether to store preimage of trie key to the disk
}

var DefaultCacheConfig = &CacheConfig{
	TrieCleanLimit: 256,
	TrieDirtyLimit: 256,
	SnapshotLimit:  256,
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

	chainmu sync.RWMutex // blockchain insertion lock

	currentBlock atomic.Value // Current head of the block chain

	stateCache    state.Database // State database to reuse between imports (contains state cache)
	stateManager  TrieWriter
	bodyCache     *lru.Cache // Cache for the most recent block bodies
	receiptsCache *lru.Cache // Cache for the most recent receipts per block
	blockCache    *lru.Cache // Cache for the most recent entire blocks
	txLookupCache *lru.Cache // Cache for the most recent transaction lookup data.

	quit    chan struct{}  // blockchain quit channel
	wg      sync.WaitGroup // chain processing wait group for shutting down
	running int32          // 0 if chain is running, 1 when stopped

	engine     consensus.Engine
	validator  Validator  // Block and state validator interface
	prefetcher Prefetcher // Block state prefetcher interface
	processor  Processor  // Block transaction processor interface
	vmConfig   vm.Config

	badBlocks *lru.Cache // Bad block cache

	lastAccepted *types.Block // Prevents reorgs past this height
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
		quit:          make(chan struct{}),
		bodyCache:     bodyCache,
		receiptsCache: receiptsCache,
		blockCache:    blockCache,
		txLookupCache: txLookupCache,
		engine:        engine,
		vmConfig:      vmConfig,
		badBlocks:     badBlocks,
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

	if err := bc.loadLastState(lastAcceptedHash); err != nil {
		return nil, err
	}
	// Create the state manager
	bc.stateManager = NewTrieWriter(bc.stateCache.TrieDB(), cacheConfig)

	// Make sure the state associated with the block is available
	head := bc.CurrentBlock()
	if _, err := state.New(head.Root(), bc.stateCache, nil); err != nil {
		return nil, fmt.Errorf("head state missing %d:%s", head.Number(), head.Hash())
	}

	// Load any existing snapshot, regenerating it if loading failed
	if bc.cacheConfig.SnapshotLimit > 0 {
		// If we are starting from genesis, generate the original snapshot disk layer
		// up front, so we can use it while executing blocks in bootstrapping. This
		// also avoids a costly async generation process when reaching tip.
		async := bc.cacheConfig.SnapshotAsync && head.NumberU64() > 0
		log.Info("Initializing snapshots", "async", async)
		bc.snaps, err = snapshot.New(bc.db, bc.stateCache.TrieDB(), bc.cacheConfig.SnapshotLimit, head.Hash(), head.Root(), async, true, bc.cacheConfig.SnapshotVerify)
		if err != nil {
			log.Error("failed to initialize snapshots", "headHash", head.Hash(), "headRoot", head.Root(), "err", err, "async", async)
		}
	}

	return bc, nil
}

// GetVMConfig returns the block chain VM config.
func (bc *BlockChain) GetVMConfig() *vm.Config {
	return &bc.vmConfig
}

// empty returns an indicator whether the blockchain is empty.
func (bc *BlockChain) empty() bool {
	genesis := bc.genesisBlock.Hash()
	for _, hash := range []common.Hash{rawdb.ReadHeadBlockHash(bc.db), rawdb.ReadHeadHeaderHash(bc.db), rawdb.ReadHeadFastBlockHash(bc.db)} {
		if hash != genesis {
			return false
		}
	}
	return true
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

	headerTd := bc.GetTd(currentHeader.Hash(), currentHeader.Number.Uint64())
	blockTd := bc.GetTd(currentBlock.Hash(), currentBlock.NumberU64())

	log.Info("Loaded most recent local header", "number", currentHeader.Number, "hash", currentHeader.Hash(), "td", headerTd, "age", common.PrettyAge(time.Unix(int64(currentHeader.Time), 0)))
	log.Info("Loaded most recent local full block", "number", currentBlock.Number(), "hash", currentBlock.Hash(), "td", blockTd, "age", common.PrettyAge(time.Unix(int64(currentBlock.Time()), 0)))

	// Otherwise, set the last accepted block and perform a re-org.
	bc.lastAccepted = bc.GetBlockByHash(lastAcceptedHash)
	if bc.lastAccepted == nil {
		return fmt.Errorf("could not load last accepted block")
	}

	// Remove all processing transaction indices leftover from when we used to
	// write transaction indices as soon as a block was verified.
	indicesRemoved, err := bc.db.Has(removeTxIndicesKey)
	if err != nil {
		return fmt.Errorf("unable to determine if transaction indices removed: %w", err)
	}
	if !indicesRemoved {
		indicesRemoved, err := bc.removeIndices(currentBlock.NumberU64(), bc.lastAccepted.NumberU64())
		if err != nil {
			return err
		}
		if err := bc.db.Put(removeTxIndicesKey, bc.lastAccepted.Number().Bytes()); err != nil {
			return fmt.Errorf("unable to mark indices removed: %w", err)
		}
		log.Debug("removed processing transaction indices", "count", indicesRemoved, "currentBlock", currentBlock.NumberU64(), "lastAccepted", bc.lastAccepted.NumberU64())
	}

	// This ensures that the head block is updated to the last accepted block on startup
	if err := bc.setPreference(bc.lastAccepted); err != nil {
		return fmt.Errorf("failed to set preference to last accepted block while loading last state: %w", err)
	}

	// reprocessState as necessary to ensure that the last accepted state is
	// available. The state may not be available if it was not committed due
	// to an unclean shutdown.
	return bc.reprocessState(bc.lastAccepted, 2*commitInterval, true)
}

// removeIndices removes all transaction lookup entries for the transactions contained in the canonical chain
// from block at height [to] to block at height [from]. Blocks are traversed in reverse order.
func (bc *BlockChain) removeIndices(from, to uint64) (int, error) {
	indicesRemoved := 0
	batch := bc.db.NewBatch()
	for i := from; i > to; i-- {
		b := bc.GetBlockByNumber(i)
		if b == nil {
			return indicesRemoved, fmt.Errorf("could not load canonical block at height %d", i)
		}
		for _, tx := range b.Transactions() {
			rawdb.DeleteTxLookupEntry(batch, tx.Hash())
			indicesRemoved++
		}
	}
	if err := batch.Write(); err != nil {
		return 0, fmt.Errorf("failed to write batch while removing indices (from: %d, to: %d): %w", from, to, err)
	}
	return indicesRemoved, nil
}

// GasLimit returns the gas limit of the current HEAD block.
func (bc *BlockChain) GasLimit() uint64 {
	return bc.CurrentBlock().GasLimit()
}

// CurrentBlock retrieves the current head block of the canonical chain. The
// block is retrieved from the blockchain's internal cache.
func (bc *BlockChain) CurrentBlock() *types.Block {
	return bc.currentBlock.Load().(*types.Block)
}

// Snapshots returns the blockchain snapshot tree.
func (bc *BlockChain) Snapshots() *snapshot.Tree {
	return bc.snaps
}

// Validator returns the current validator.
func (bc *BlockChain) Validator() Validator {
	return bc.validator
}

// Processor returns the current processor.
func (bc *BlockChain) Processor() Processor {
	return bc.processor
}

// State returns a new mutable state based on the current HEAD block.
func (bc *BlockChain) State() (*state.StateDB, error) {
	return bc.StateAt(bc.CurrentBlock().Root())
}

// StateAt returns a new mutable state based on a particular point in time.
func (bc *BlockChain) StateAt(root common.Hash) (*state.StateDB, error) {
	return state.New(root, bc.stateCache, bc.snaps)
}

// StateCache returns the caching database underpinning the blockchain instance.
func (bc *BlockChain) StateCache() state.Database {
	return bc.stateCache
}

func (bc *BlockChain) loadGenesisState() error {
	// Prepare the genesis block and reinitialise the chain
	batch := bc.db.NewBatch()
	rawdb.WriteTd(batch, bc.genesisBlock.Hash(), bc.genesisBlock.NumberU64(), bc.genesisBlock.Difficulty())
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
// header and the head fast sync block to this very same block if they are older
// or if they are on a different side chain.
//
// Note, this function assumes that the `mu` mutex is held!
func (bc *BlockChain) writeHeadBlock(block *types.Block) {
	// If the block is on a side chain or an unknown one, force other heads onto it too
	updateHeads := rawdb.ReadCanonicalHash(bc.db, block.NumberU64()) != block.Hash()

	// Add the block to the canonical chain number scheme and mark as the head
	batch := bc.db.NewBatch()
	rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(batch, block.Hash())

	// If the block is better than our head or is on a different chain, force update heads
	if updateHeads {
		rawdb.WriteHeadHeaderHash(batch, block.Hash())
		rawdb.WriteHeadFastBlockHash(batch, block.Hash())
	}
	// Flush the whole batch into the disk, exit the node if failed
	if err := batch.Write(); err != nil {
		log.Crit("Failed to update chain indexes and markers", "err", err)
	}
	// Update all in-memory chain markers in the last step
	if updateHeads {
		bc.hc.SetCurrentHeader(block.Header())
	}
	bc.currentBlock.Store(block)
}

// Genesis retrieves the chain's genesis block.
func (bc *BlockChain) Genesis() *types.Block {
	return bc.genesisBlock
}

// GetBody retrieves a block body (transactions and uncles) from the database by
// hash, caching it if found.
func (bc *BlockChain) GetBody(hash common.Hash) *types.Body {
	// Short circuit if the body's already in the cache, retrieve otherwise
	if cached, ok := bc.bodyCache.Get(hash); ok {
		body := cached.(*types.Body)
		return body
	}
	number := bc.hc.GetBlockNumber(hash)
	if number == nil {
		return nil
	}
	body := rawdb.ReadBody(bc.db, hash, *number)
	if body == nil {
		return nil
	}
	// Cache the found body for next time and return
	bc.bodyCache.Add(hash, body)
	return body
}

// HasBlock checks if a block is fully present in the database or not.
func (bc *BlockChain) HasBlock(hash common.Hash, number uint64) bool {
	if bc.blockCache.Contains(hash) {
		return true
	}
	return rawdb.HasBody(bc.db, hash, number)
}

// HasState checks if state trie is fully present in the database or not.
func (bc *BlockChain) HasState(hash common.Hash) bool {
	_, err := bc.stateCache.OpenTrie(hash)
	return err == nil
}

// HasBlockAndState checks if a block and associated state trie is fully present
// in the database or not, caching it if present.
func (bc *BlockChain) HasBlockAndState(hash common.Hash, number uint64) bool {
	// Check first that the block itself is known
	block := bc.GetBlock(hash, number)
	if block == nil {
		return false
	}
	return bc.HasState(block.Root())
}

// GetBlock retrieves a block from the database by hash and number,
// caching it if found.
func (bc *BlockChain) GetBlock(hash common.Hash, number uint64) *types.Block {
	// Short circuit if the block's already in the cache, retrieve otherwise
	if block, ok := bc.blockCache.Get(hash); ok {
		return block.(*types.Block)
	}
	block := rawdb.ReadBlock(bc.db, hash, number)
	if block == nil {
		return nil
	}
	// Cache the found block for next time and return
	bc.blockCache.Add(block.Hash(), block)
	return block
}

// GetBlockByHash retrieves a block from the database by hash, caching it if found.
func (bc *BlockChain) GetBlockByHash(hash common.Hash) *types.Block {
	number := bc.hc.GetBlockNumber(hash)
	if number == nil {
		return nil
	}
	return bc.GetBlock(hash, *number)
}

// GetBlockByNumber retrieves a block from the database by number, caching it
// (associated with its hash) if found.
func (bc *BlockChain) GetBlockByNumber(number uint64) *types.Block {
	hash := rawdb.ReadCanonicalHash(bc.db, number)
	if hash == (common.Hash{}) {
		return nil
	}
	return bc.GetBlock(hash, number)
}

// ValidateCanonicalChain confirms a canonical chain is well-formed.
func (bc *BlockChain) ValidateCanonicalChain() error {
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

// GetReceiptsByHash retrieves the receipts for all transactions in a given block.
func (bc *BlockChain) GetReceiptsByHash(hash common.Hash) types.Receipts {
	if receipts, ok := bc.receiptsCache.Get(hash); ok {
		return receipts.(types.Receipts)
	}
	number := rawdb.ReadHeaderNumber(bc.db, hash)
	if number == nil {
		return nil
	}
	receipts := rawdb.ReadReceipts(bc.db, hash, *number, bc.chainConfig)
	if receipts == nil {
		return nil
	}
	bc.receiptsCache.Add(hash, receipts)
	return receipts
}

// GetBlocksFromHash returns the block corresponding to hash and up to n-1 ancestors.
// [deprecated by eth/62]
func (bc *BlockChain) GetBlocksFromHash(hash common.Hash, n int) (blocks []*types.Block) {
	number := bc.hc.GetBlockNumber(hash)
	if number == nil {
		return nil
	}
	for i := 0; i < n; i++ {
		block := bc.GetBlock(hash, *number)
		if block == nil {
			break
		}
		blocks = append(blocks, block)
		hash = block.ParentHash()
		*number--
	}
	return
}

// GetUnclesInChain retrieves all the uncles from a given block backwards until
// a specific distance is reached.
func (bc *BlockChain) GetUnclesInChain(block *types.Block, length int) []*types.Header {
	uncles := []*types.Header{}
	for i := 0; block != nil && i < length; i++ {
		uncles = append(uncles, block.Uncles()...)
		block = bc.GetBlock(block.ParentHash(), block.NumberU64()-1)
	}
	return uncles
}

// TrieNode retrieves a blob of data associated with a trie node
// either from ephemeral in-memory cache, or from persistent storage.
func (bc *BlockChain) TrieNode(hash common.Hash) ([]byte, error) {
	return bc.stateCache.TrieDB().Node(hash)
}

// ContractCode retrieves a blob of data associated with a contract hash
// either from ephemeral in-memory cache, or from persistent storage.
func (bc *BlockChain) ContractCode(hash common.Hash) ([]byte, error) {
	return bc.stateCache.ContractCode(common.Hash{}, hash)
}

// Stop stops the blockchain service. If any imports are currently in progress
// it will abort them using the procInterrupt.
func (bc *BlockChain) Stop() {
	if !atomic.CompareAndSwapInt32(&bc.running, 0, 1) {
		return
	}
	// Unsubscribe all subscriptions registered from blockchain
	bc.scope.Close()
	close(bc.quit)
	bc.wg.Wait()

	if err := bc.stateManager.Shutdown(); err != nil {
		log.Error("Failed to Shutdown state manager", "err", err)
	}

	log.Info("Blockchain stopped")
}

// WriteStatus status of write
type WriteStatus byte

const (
	NonStatTy WriteStatus = iota
	CanonStatTy
	SideStatTy
)

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

	// Send an ChainHeadEvent if we end up altering
	// the head block. Many internal aysnc processes rely on
	// receiving these events (i.e. the TxPool).
	bc.chainHeadFeed.Send(ChainHeadEvent{Block: block})
	return nil
}

// LastAcceptedBlock returns the last block to be marked as accepted.
func (bc *BlockChain) LastAcceptedBlock() *types.Block {
	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	return bc.lastAccepted
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

	// Abort snapshot generation before pruning anything from trie database
	// (could occur in AcceptTrie)
	if bc.snaps != nil {
		bc.snaps.AbortGeneration()
	}

	// Accept Trie
	if err := bc.stateManager.AcceptTrie(block); err != nil {
		return fmt.Errorf("unable to accept trie: %w", err)
	}

	// Flatten the entire snap Trie to disk
	if bc.snaps != nil {
		if err := bc.snaps.Flatten(block.Hash()); err != nil {
			return fmt.Errorf("unable to flatten trie: %w", err)
		}
	}

	// Update transaction lookup index
	batch := bc.db.NewBatch()
	rawdb.WriteTxLookupEntriesByBlock(batch, block)
	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write tx lookup entries batch: %w", err)
	}

	// Fetch block logs
	logs := bc.gatherBlockLogs(block.Hash(), block.NumberU64(), false)

	// Update accepted feeds
	bc.chainAcceptedFeed.Send(ChainEvent{Block: block, Hash: block.Hash(), Logs: logs})
	if len(logs) > 0 {
		bc.logsAcceptedFeed.Send(logs)
	}
	if len(block.Transactions()) != 0 {
		bc.txAcceptedFeed.Send(NewTxsEvent{block.Transactions()})
	}

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
	bc.wg.Add(1)
	defer bc.wg.Done()

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

// writeBlockWithState writes the block and all associated state to the database,
// but it expects the chain mutex to be held.
// writeBlockWithState expects to be the last verification step during InsertBlock
// since it creates a reference that will only be cleaned up by Accept/Reject.
func (bc *BlockChain) writeBlockWithState(block *types.Block, receipts []*types.Receipt, logs []*types.Log, state *state.StateDB) (WriteStatus, error) {
	bc.wg.Add(1)
	defer bc.wg.Done()

	// Calculate the total difficulty of the block
	ptd := bc.GetTd(block.ParentHash(), block.NumberU64()-1)
	if ptd == nil {
		return NonStatTy, consensus.ErrUnknownAncestor
	}
	// Make sure no inconsistent state is leaked during insertion
	// currentBlock := bc.CurrentBlock()
	// localTd := bc.GetTd(currentBlock.Hash(), currentBlock.NumberU64())
	externTd := new(big.Int).Add(block.Difficulty(), ptd)

	// Irrelevant of the canonical status, write the block itself to the database.
	//
	// Note all the components of block(td, hash->number map, header, body, receipts)
	// should be written atomically. BlockBatch is used for containing all components.
	blockBatch := bc.db.NewBatch()
	rawdb.WriteTd(blockBatch, block.Hash(), block.NumberU64(), externTd)
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
		return NonStatTy, err
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
		return NonStatTy, err
	}

	// If [block] represents a new tip of the canonical chain, we optimistically add it before
	// setPreference is called. Otherwise, we consider it a side chain block.
	if bc.newTip(block) {
		bc.writeCanonicalBlockWithLogs(block, logs)
		return CanonStatTy, nil
	}

	bc.chainSideFeed.Send(ChainSideEvent{Block: block})
	return SideStatTy, nil
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

	// Remove already known canon-blocks
	var (
		block, prev *types.Block
	)
	// Do a sanity check that the provided chain is actually ordered and linked
	for i := 1; i < len(chain); i++ {
		block = chain[i]
		prev = chain[i-1]
		if block.NumberU64() != prev.NumberU64()+1 || block.ParentHash() != prev.Hash() {
			// Chain broke ancestry, log a message (programming error) and skip insertion
			log.Error("Non contiguous block insert", "number", block.Number(), "hash", block.Hash(),
				"parent", block.ParentHash(), "prevnumber", prev.Number(), "prevhash", prev.Hash())

			return 0, fmt.Errorf("non contiguous insert: item %d is #%d [%x…], item %d is #%d [%x…] (parent [%x…])", i-1, prev.NumberU64(),
				prev.Hash().Bytes()[:4], i, block.NumberU64(), block.Hash().Bytes()[:4], block.ParentHash().Bytes()[:4])
		}
	}
	// Pre-checks passed, start the full block imports
	bc.wg.Add(1)
	defer bc.wg.Done()

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

	bc.wg.Add(1)
	bc.chainmu.Lock()
	err := bc.insertBlock(block, writes)
	bc.chainmu.Unlock()
	bc.wg.Done()

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
	senderCacher.recover(types.MakeSigner(bc.chainConfig, block.Number(), new(big.Int).SetUint64(block.Time())), block.Transactions())

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

	// Retrieve the parent block and it's state to execute on top
	start := time.Now()

	parent := bc.GetHeader(block.ParentHash(), block.NumberU64()-1)

	var statedb *state.StateDB
	if bc.snaps == nil {
		statedb, err = state.New(parent.Root, bc.stateCache, nil)
	} else {
		snap := bc.snaps.Snapshot(parent.Root)
		if snap == nil {
			return fmt.Errorf("failed to get snapshot for parent root: %s", parent.Root)
		}
		statedb, err = state.NewWithSnapshot(parent.Root, bc.stateCache, snap)
	}
	if err != nil {
		return err
	}

	// Enable prefetching to pull in trie node paths while processing transactions
	statedb.StartPrefetcher("chain")
	activeState = statedb

	// If we have a followup block, run that against the current state to pre-cache
	// transactions and probabilistically some of the account/storage trie nodes.
	// Process block using the parent state as reference point
	receipts, logs, usedGas, err := bc.processor.Process(block, statedb, bc.vmConfig)
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
	// writeBlockWithState creates a reference that will be cleaned up in Accept/Reject
	// so we need to ensure an error cannot occur later in verification, since that would
	// cause the referenced root to never be dereferenced.
	status, err := bc.writeBlockWithState(block, receipts, logs, statedb)
	if err != nil {
		return err
	}

	switch status {
	case CanonStatTy:
		log.Debug("Inserted new block", "number", block.Number(), "hash", block.Hash(),
			"parentHash", block.ParentHash(),
			"uncles", len(block.Uncles()), "txs", len(block.Transactions()), "gas", block.GasUsed(),
			"elapsed", common.PrettyDuration(time.Since(start)),
			"root", block.Root())
		// Only count canonical blocks for GC processing time
	case SideStatTy:
		log.Debug("Inserted forked block", "number", block.Number(), "hash", block.Hash(),
			"parentHash", block.ParentHash(),
			"diff", block.Difficulty(), "elapsed", common.PrettyDuration(time.Since(start)),
			"txs", len(block.Transactions()), "gas", block.GasUsed(), "uncles", len(block.Uncles()),
			"root", block.Root())
	default:
		// This in theory is impossible, but lets be nice to our future selves and leave
		// a log, instead of trying to track down blocks imports that don't emit logs.
		log.Warn("Inserted block with unknown status", "number", block.Number(), "hash", block.Hash(),
			"parentHash", block.ParentHash(),
			"diff", block.Difficulty(), "elapsed", common.PrettyDuration(time.Since(start)),
			"txs", len(block.Transactions()), "gas", block.GasUsed(), "uncles", len(block.Uncles()),
			"root", block.Root())
	}

	return err
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

		// collectLogs collects the logs that were generated or removed during
		// the processing of the block that corresponds with the given hash.
		// These logs are later announced as deleted or reborn
		collectLogs = func(hash common.Hash, removed bool) {
			number := bc.hc.GetBlockNumber(hash)
			if number == nil {
				return
			}
			logs := bc.gatherBlockLogs(hash, *number, removed)
			if len(logs) > 0 {
				if removed {
					deletedLogs = append(deletedLogs, logs)
				} else {
					rebirthLogs = append(rebirthLogs, logs)
				}
			}
		}
		// mergeLogs returns a merged log slice with specified sort order.
		mergeLogs = func(logs [][]*types.Log, reverse bool) []*types.Log {
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
	)
	// Reduce the longer chain to the same number as the shorter one
	if oldBlock.NumberU64() > newBlock.NumberU64() {
		// Old chain is longer, gather all transactions and logs as deleted ones
		for ; oldBlock != nil && oldBlock.NumberU64() != newBlock.NumberU64(); oldBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1) {
			oldChain = append(oldChain, oldBlock)
			collectLogs(oldBlock.Hash(), true)
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
		collectLogs(oldBlock.Hash(), true)

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
		return errors.New("cannot orphan finalized block")
	}

	// Ensure the user sees large reorgs
	if len(oldChain) > 0 && len(newChain) > 0 {
		logFn := log.Info
		msg := "Chain reorg detected"
		if len(oldChain) > 63 {
			msg = "Large chain reorg detected"
			logFn = log.Warn
		}
		logFn(msg, "number", commonBlock.Number(), "hash", commonBlock.Hash(),
			"drop", len(oldChain), "dropfrom", oldChain[0].Hash(), "add", len(newChain), "addfrom", newChain[0].Hash())
	} else {
		log.Warn("Unlikely reorg (rewind to ancestor) occurred", "oldnum", oldHead.Number(), "oldhash", oldHead.Hash(), "newnum", newHead.Number(), "newhash", newHead.Hash())
	}
	// Insert the new chain(except the head block(reverse order)),
	// taking care of the proper incremental order.
	for i := len(newChain) - 1; i >= 1; i-- {
		// Insert the block in the canonical way, re-writing history
		bc.writeHeadBlock(newChain[i])

		// Collect reborn logs due to chain reorg
		collectLogs(newChain[i].Hash(), false)
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

// CurrentHeader retrieves the current head header of the canonical chain. The
// header is retrieved from the HeaderChain's internal cache.
func (bc *BlockChain) CurrentHeader() *types.Header {
	return bc.hc.CurrentHeader()
}

// GetTd retrieves a block's total difficulty in the canonical chain from the
// database by hash and number, caching it if found.
func (bc *BlockChain) GetTd(hash common.Hash, number uint64) *big.Int {
	return bc.hc.GetTd(hash, number)
}

// GetTdByHash retrieves a block's total difficulty in the canonical chain from the
// database by hash, caching it if found.
func (bc *BlockChain) GetTdByHash(hash common.Hash) *big.Int {
	return bc.hc.GetTdByHash(hash)
}

// GetHeader retrieves a block header from the database by hash and number,
// caching it if found.
func (bc *BlockChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	// Blockchain might have cached the whole block, only if not go to headerchain
	if block, ok := bc.blockCache.Get(hash); ok {
		return block.(*types.Block).Header()
	}

	return bc.hc.GetHeader(hash, number)
}

// GetHeaderByHash retrieves a block header from the database by hash, caching it if
// found.
func (bc *BlockChain) GetHeaderByHash(hash common.Hash) *types.Header {
	// Blockchain might have cached the whole block, only if not go to headerchain
	if block, ok := bc.blockCache.Get(hash); ok {
		return block.(*types.Block).Header()
	}

	return bc.hc.GetHeaderByHash(hash)
}

// HasHeader checks if a block header is present in the database or not, caching
// it if present.
func (bc *BlockChain) HasHeader(hash common.Hash, number uint64) bool {
	return bc.hc.HasHeader(hash, number)
}

// GetCanonicalHash returns the canonical hash for a given block number
func (bc *BlockChain) GetCanonicalHash(number uint64) common.Hash {
	return bc.hc.GetCanonicalHash(number)
}

// GetHeaderByNumber retrieves a block header from the database by number,
// caching it (associated with its hash) if found.
func (bc *BlockChain) GetHeaderByNumber(number uint64) *types.Header {
	return bc.hc.GetHeaderByNumber(number)
}

// GetTransactionLookup retrieves the lookup associate with the given transaction
// hash from the cache or database.
func (bc *BlockChain) GetTransactionLookup(hash common.Hash) *rawdb.LegacyTxLookupEntry {
	// Short circuit if the txlookup already in the cache, retrieve otherwise
	if lookup, exist := bc.txLookupCache.Get(hash); exist {
		return lookup.(*rawdb.LegacyTxLookupEntry)
	}
	tx, blockHash, blockNumber, txIndex := rawdb.ReadTransaction(bc.db, hash)
	if tx == nil {
		return nil
	}
	lookup := &rawdb.LegacyTxLookupEntry{BlockHash: blockHash, BlockIndex: blockNumber, Index: txIndex}
	bc.txLookupCache.Add(hash, lookup)
	return lookup
}

// Config retrieves the chain's fork configuration.
func (bc *BlockChain) Config() *params.ChainConfig { return bc.chainConfig }

// Engine retrieves the blockchain's consensus engine.
func (bc *BlockChain) Engine() consensus.Engine { return bc.engine }

// SubscribeRemovedLogsEvent registers a subscription of RemovedLogsEvent.
func (bc *BlockChain) SubscribeRemovedLogsEvent(ch chan<- RemovedLogsEvent) event.Subscription {
	return bc.scope.Track(bc.rmLogsFeed.Subscribe(ch))
}

// SubscribeChainEvent registers a subscription of ChainEvent.
func (bc *BlockChain) SubscribeChainEvent(ch chan<- ChainEvent) event.Subscription {
	return bc.scope.Track(bc.chainFeed.Subscribe(ch))
}

// SubscribeChainAcceptedEvent registers a subscription of ChainEvent.
func (bc *BlockChain) SubscribeChainAcceptedEvent(ch chan<- ChainEvent) event.Subscription {
	return bc.scope.Track(bc.chainAcceptedFeed.Subscribe(ch))
}

// SubscribeChainHeadEvent registers a subscription of ChainHeadEvent.
func (bc *BlockChain) SubscribeChainHeadEvent(ch chan<- ChainHeadEvent) event.Subscription {
	return bc.scope.Track(bc.chainHeadFeed.Subscribe(ch))
}

// SubscribeChainSideEvent registers a subscription of ChainSideEvent.
func (bc *BlockChain) SubscribeChainSideEvent(ch chan<- ChainSideEvent) event.Subscription {
	return bc.scope.Track(bc.chainSideFeed.Subscribe(ch))
}

// SubscribeLogsEvent registers a subscription of []*types.Log.
func (bc *BlockChain) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return bc.scope.Track(bc.logsFeed.Subscribe(ch))
}

// SubscribeAcceptedLogsEvent registers a subscription of accepted []*types.Log.
func (bc *BlockChain) SubscribeAcceptedLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return bc.scope.Track(bc.logsAcceptedFeed.Subscribe(ch))
}

// SubscribeBlockProcessingEvent registers a subscription of bool where true means
// block processing has started while false means it has stopped.
func (bc *BlockChain) SubscribeBlockProcessingEvent(ch chan<- bool) event.Subscription {
	return bc.scope.Track(bc.blockProcFeed.Subscribe(ch))
}

// SubscribeAcceptedTransactionEvent registers a subscription of accepted transactions
func (bc *BlockChain) SubscribeAcceptedTransactionEvent(ch chan<- NewTxsEvent) event.Subscription {
	return bc.scope.Track(bc.txAcceptedFeed.Subscribe(ch))
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

// reprocessState reprocesses the state up to [block], iterating through its ancestors until
// it reaches a block with a state committed to the database. reprocessState does not use
// snapshots since the disk layer for snapshots will most likely be above the last committed
// state that reprocessing will start from.
func (bc *BlockChain) reprocessState(current *types.Block, reexec uint64, report bool) error {
	var (
		origin = current.NumberU64()
	)
	// If the state is already available, skip re-processing
	statedb, err := state.New(current.Root(), bc.stateCache, nil)
	if err == nil {
		return nil
	}
	// Check how far back we need to re-execute, capped at [reexec]
	for i := 0; i < int(reexec); i++ {
		if current.NumberU64() == 0 {
			return errors.New("genesis state is missing")
		}
		parent := bc.GetBlock(current.ParentHash(), current.NumberU64()-1)
		if parent == nil {
			return fmt.Errorf("missing block %s:%d", current.ParentHash().Hex(), current.NumberU64()-1)
		}
		current = parent

		statedb, err = state.New(current.Root(), bc.stateCache, nil)
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
	)
	// Note: we add 1 since in each iteration, we attempt to re-execute the next block.
	log.Info("Re-executing blocks to generate state for last accepted block", "from", current.NumberU64()+1, "to", origin)
	for current.NumberU64() < origin {
		// Print progress logs if long enough time elapsed
		if time.Since(logged) > 8*time.Second && report {
			log.Info("Regenerating historical state", "block", current.NumberU64()+1, "target", origin, "remaining", origin-current.NumberU64(), "elapsed", time.Since(start))
			logged = time.Now()
		}
		// Retrieve the next block to regenerate and process it
		next := current.NumberU64() + 1
		if current = bc.GetBlockByNumber(next); current == nil {
			return fmt.Errorf("failed to retrieve block %d while re-generating state", next)
		}
		receipts, _, usedGas, err := bc.processor.Process(current, statedb, vm.Config{})
		if err != nil {
			return fmt.Errorf("failed to re-process block (%s: %d): %v", current.Hash().Hex(), current.NumberU64(), err)
		}
		// Validate the state using the default validator
		if err := bc.validator.ValidateState(current, statedb, receipts, usedGas); err != nil {
			return fmt.Errorf("failed to validate state while re-processing block (%s: %d): %v", current.Hash().Hex(), current.NumberU64(), err)
		}
		log.Debug("processed block", "block", current.Hash(), "number", current.NumberU64())
		// Finalize the state so any modifications are written to the trie
		root, err := statedb.Commit(bc.chainConfig.IsEIP158(current.Number()))
		if err != nil {
			return err
		}
		statedb, err = state.New(root, bc.stateCache, nil)
		if err != nil {
			return fmt.Errorf("state reset after block %d failed: %v", current.NumberU64(), err)
		}

		triedb.Reference(root, common.Hash{})
		if previousRoot != (common.Hash{}) {
			triedb.Dereference(previousRoot)
		}
		previousRoot = root
	}
	if report {
		nodes, imgs := triedb.Size()
		log.Info("Historical state regenerated", "block", current.NumberU64(), "elapsed", time.Since(start), "nodes", nodes, "preimages", imgs)
	}
	if previousRoot != (common.Hash{}) {
		return triedb.Commit(previousRoot, report, nil)
	}
	return nil
}
