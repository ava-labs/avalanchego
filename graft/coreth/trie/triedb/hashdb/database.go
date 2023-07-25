// (c) 2020-2022, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2018 The go-ethereum Authors
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

package hashdb

import (
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ava-labs/coreth/trie/trienode"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	memcacheCleanHitMeter   = metrics.NewRegisteredMeter("trie/memcache/clean/hit", nil)
	memcacheCleanMissMeter  = metrics.NewRegisteredMeter("trie/memcache/clean/miss", nil)
	memcacheCleanReadMeter  = metrics.NewRegisteredMeter("trie/memcache/clean/read", nil)
	memcacheCleanWriteMeter = metrics.NewRegisteredMeter("trie/memcache/clean/write", nil)

	memcacheDirtyHitMeter       = metrics.NewRegisteredMeter("trie/memcache/dirty/hit", nil)
	memcacheDirtyMissMeter      = metrics.NewRegisteredMeter("trie/memcache/dirty/miss", nil)
	memcacheDirtyReadMeter      = metrics.NewRegisteredMeter("trie/memcache/dirty/read", nil)
	memcacheDirtyWriteMeter     = metrics.NewRegisteredMeter("trie/memcache/dirty/write", nil)
	memcacheDirtySizeGauge      = metrics.NewRegisteredGaugeFloat64("trie/memcache/dirty/size", nil)
	memcacheDirtyChildSizeGauge = metrics.NewRegisteredGaugeFloat64("trie/memcache/dirty/childsize", nil)
	memcacheDirtyNodesGauge     = metrics.NewRegisteredGauge("trie/memcache/dirty/nodes", nil)

	memcacheFlushMeter         = metrics.NewRegisteredMeter("trie/memcache/flush/count", nil)
	memcacheFlushTimeTimer     = metrics.NewRegisteredResettingTimer("trie/memcache/flush/time", nil)
	memcacheFlushLockTimeTimer = metrics.NewRegisteredResettingTimer("trie/memcache/flush/locktime", nil)
	memcacheFlushNodesMeter    = metrics.NewRegisteredMeter("trie/memcache/flush/nodes", nil)
	memcacheFlushSizeMeter     = metrics.NewRegisteredMeter("trie/memcache/flush/size", nil)

	memcacheGCTimeTimer  = metrics.NewRegisteredResettingTimer("trie/memcache/gc/time", nil)
	memcacheGCNodesMeter = metrics.NewRegisteredMeter("trie/memcache/gc/nodes", nil)
	memcacheGCSizeMeter  = metrics.NewRegisteredMeter("trie/memcache/gc/size", nil)

	memcacheCommitMeter         = metrics.NewRegisteredMeter("trie/memcache/commit/count", nil)
	memcacheCommitTimeTimer     = metrics.NewRegisteredResettingTimer("trie/memcache/commit/time", nil)
	memcacheCommitLockTimeTimer = metrics.NewRegisteredResettingTimer("trie/memcache/commit/locktime", nil)
	memcacheCommitNodesMeter    = metrics.NewRegisteredMeter("trie/memcache/commit/nodes", nil)
	memcacheCommitSizeMeter     = metrics.NewRegisteredMeter("trie/memcache/commit/size", nil)
)

// ChildResolver defines the required method to decode the provided
// trie node and iterate the children on top.
type ChildResolver interface {
	ForEach(node []byte, onChild func(common.Hash))
}

type cache interface {
	HasGet([]byte, []byte) ([]byte, bool)
	Del([]byte)
	Set([]byte, []byte)
}

// Database is an intermediate write layer between the trie data structures and
// the disk database. The aim is to accumulate trie writes in-memory and only
// periodically flush a couple tries to disk, garbage collecting the remainder.
//
// The trie Database is thread-safe in its mutations and is thread-safe in providing individual,
// independent node access.
type Database struct {
	diskdb   ethdb.Database // Persistent storage for matured trie nodes
	resolver ChildResolver  // The handler to resolve children of nodes

	cleans  cache                       // GC friendly memory cache of clean node RLPs
	dirties map[common.Hash]*cachedNode // Data and references relationships of dirty trie nodes
	oldest  common.Hash                 // Oldest tracked node, flush-list head
	newest  common.Hash                 // Newest tracked node, flush-list tail

	gctime  time.Duration      // Time spent on garbage collection since last commit
	gcnodes uint64             // Nodes garbage collected since last commit
	gcsize  common.StorageSize // Data storage garbage collected since last commit

	flushtime  time.Duration      // Time spent on data flushing since last commit
	flushnodes uint64             // Nodes flushed since last commit
	flushsize  common.StorageSize // Data storage flushed since last commit

	dirtiesSize  common.StorageSize // Storage size of the dirty node cache (exc. metadata)
	childrenSize common.StorageSize // Storage size of the external children tracking

	lock sync.RWMutex
}

// cachedNode is all the information we know about a single cached trie node
// in the memory database write layer.
type cachedNode struct {
	node      []byte                   // Encoded node blob
	parents   uint32                   // Number of live nodes referencing this one
	external  map[common.Hash]struct{} // The set of external children
	flushPrev common.Hash              // Previous node in the flush-list
	flushNext common.Hash              // Next node in the flush-list
}

// cachedNodeSize is the raw size of a cachedNode data structure without any
// node data included. It's an approximate size, but should be a lot better
// than not counting them.
var cachedNodeSize = int(reflect.TypeOf(cachedNode{}).Size())

// forChildren invokes the callback for all the tracked children of this node,
// both the implicit ones from inside the node as well as the explicit ones
// from outside the node.
func (n *cachedNode) forChildren(resolver ChildResolver, onChild func(hash common.Hash)) {
	for child := range n.external {
		onChild(child)
	}
	resolver.ForEach(n.node, onChild)
}

// New initializes the hash-based node database.
func New(diskdb ethdb.Database, cleans cache, resolver ChildResolver) *Database {
	return &Database{
		diskdb:   diskdb,
		resolver: resolver,
		cleans:   cleans,
		dirties:  make(map[common.Hash]*cachedNode),
	}
}

// insert inserts a simplified trie node into the memory database.
// All nodes inserted by this function will be reference tracked
// and in theory should only used for **trie nodes** insertion.
func (db *Database) insert(hash common.Hash, node []byte) {
	// If the node's already cached, skip
	if _, ok := db.dirties[hash]; ok {
		return
	}
	memcacheDirtyWriteMeter.Mark(int64(len(node)))

	// Create the cached entry for this node
	entry := &cachedNode{
		node:      node,
		flushPrev: db.newest,
	}
	entry.forChildren(db.resolver, func(child common.Hash) {
		if c := db.dirties[child]; c != nil {
			c.parents++
		}
	})
	db.dirties[hash] = entry

	// Update the flush-list endpoints
	if db.oldest == (common.Hash{}) {
		db.oldest, db.newest = hash, hash
	} else {
		db.dirties[db.newest].flushNext, db.newest = hash, hash
	}
	db.dirtiesSize += common.StorageSize(common.HashLength + len(node))
}

// Node retrieves an encoded cached trie node from memory. If it cannot be found
// cached, the method queries the persistent database for the content.
func (db *Database) Node(hash common.Hash) ([]byte, error) {
	// It doesn't make sense to retrieve the metaroot
	if hash == (common.Hash{}) {
		return nil, errors.New("not found")
	}
	// Retrieve the node from the clean cache if available
	if db.cleans != nil {
		k := hash[:]
		enc, found := db.cleans.HasGet(nil, k)
		if found {
			if len(enc) > 0 {
				memcacheCleanHitMeter.Mark(1)
				memcacheCleanReadMeter.Mark(int64(len(enc)))
				return enc, nil
			} else {
				// Delete anything from cache that may have been added incorrectly
				//
				// This will prevent a panic as callers of this function assume the raw
				// or cached node is populated.
				log.Debug("removing empty value found in cleans cache", "k", k)
				db.cleans.Del(k)
			}
		}
	}
	// Retrieve the node from the dirty cache if available
	db.lock.RLock()
	dirty := db.dirties[hash]
	db.lock.RUnlock()

	if dirty != nil {
		memcacheDirtyHitMeter.Mark(1)
		memcacheDirtyReadMeter.Mark(int64(len(dirty.node)))
		return dirty.node, nil
	}
	memcacheDirtyMissMeter.Mark(1)

	// Content unavailable in memory, attempt to retrieve from disk
	enc := rawdb.ReadLegacyTrieNode(db.diskdb, hash)
	if len(enc) > 0 {
		if db.cleans != nil {
			db.cleans.Set(hash[:], enc)
			memcacheCleanMissMeter.Mark(1)
			memcacheCleanWriteMeter.Mark(int64(len(enc)))
		}
		return enc, nil
	}
	return nil, errors.New("not found")
}

// Nodes retrieves the hashes of all the nodes cached within the memory database.
// This method is extremely expensive and should only be used to validate internal
// states in test code.
func (db *Database) Nodes() []common.Hash {
	db.lock.RLock()
	defer db.lock.RUnlock()

	var hashes = make([]common.Hash, 0, len(db.dirties))
	for hash := range db.dirties {
		hashes = append(hashes, hash)
	}
	return hashes
}

// Reference adds a new reference from a parent node to a child node.
// This function is used to add reference between internal trie node
// and external node(e.g. storage trie root), all internal trie nodes
// are referenced together by database itself.
func (db *Database) Reference(child common.Hash, parent common.Hash) {
	db.lock.Lock()
	defer db.lock.Unlock()

	db.reference(child, parent)
}

func (db *Database) reference(child common.Hash, parent common.Hash) {
	// If the node does not exist, it's a node pulled from disk, skip
	node, ok := db.dirties[child]
	if !ok {
		return
	}
	// The reference is for state root, increase the reference counter.
	if parent == (common.Hash{}) {
		node.parents += 1
		return
	}
	// The reference is for external storage trie, don't duplicate if
	// the reference is already existent.
	if db.dirties[parent].external == nil {
		db.dirties[parent].external = make(map[common.Hash]struct{})
	}
	if _, ok := db.dirties[parent].external[child]; ok {
		return
	}
	node.parents++
	db.dirties[parent].external[child] = struct{}{}
	db.childrenSize += common.HashLength
}

// Dereference removes an existing reference from a root node.
func (db *Database) Dereference(root common.Hash) {
	// Sanity check to ensure that the meta-root is not removed
	if root == (common.Hash{}) {
		log.Error("Attempted to dereference the trie cache meta root")
		return
	}

	db.lock.Lock()
	defer db.lock.Unlock()
	nodes, storage, start := len(db.dirties), db.dirtiesSize, time.Now()
	db.dereference(root)

	db.gcnodes += uint64(nodes - len(db.dirties))
	db.gcsize += storage - db.dirtiesSize
	db.gctime += time.Since(start)

	memcacheDirtySizeGauge.Update(float64(db.dirtiesSize))
	memcacheDirtyChildSizeGauge.Update(float64(db.childrenSize))
	memcacheDirtyNodesGauge.Update(int64(len(db.dirties)))

	memcacheGCTimeTimer.Update(time.Since(start))
	memcacheGCSizeMeter.Mark(int64(storage - db.dirtiesSize))
	memcacheGCNodesMeter.Mark(int64(nodes - len(db.dirties)))

	log.Debug("Dereferenced trie from memory database", "nodes", nodes-len(db.dirties), "size", storage-db.dirtiesSize, "time", time.Since(start),
		"gcnodes", db.gcnodes, "gcsize", db.gcsize, "gctime", db.gctime, "livenodes", len(db.dirties), "livesize", db.dirtiesSize)
}

// dereference is the private locked version of Dereference.
func (db *Database) dereference(hash common.Hash) {
	// If the node does not exist, it's a previously committed node.
	node, ok := db.dirties[hash]
	if !ok {
		return
	}
	// If there are no more references to the node, delete it and cascade
	if node.parents > 0 {
		// This is a special cornercase where a node loaded from disk (i.e. not in the
		// memcache any more) gets reinjected as a new node (short node split into full,
		// then reverted into short), causing a cached node to have no parents. That is
		// no problem in itself, but don't make maxint parents out of it.
		node.parents--
	}
	if node.parents == 0 {
		// Remove the node from the flush-list
		switch hash {
		case db.oldest:
			db.oldest = node.flushNext
			if node.flushNext != (common.Hash{}) {
				db.dirties[node.flushNext].flushPrev = common.Hash{}
			}
		case db.newest:
			db.newest = node.flushPrev
			if node.flushPrev != (common.Hash{}) {
				db.dirties[node.flushPrev].flushNext = common.Hash{}
			}
		default:
			db.dirties[node.flushPrev].flushNext = node.flushNext
			db.dirties[node.flushNext].flushPrev = node.flushPrev
		}
		// Dereference all children and delete the node
		node.forChildren(db.resolver, func(child common.Hash) {
			db.dereference(child)
		})
		delete(db.dirties, hash)
		db.dirtiesSize -= common.StorageSize(common.HashLength + len(node.node))
		if node.external != nil {
			db.childrenSize -= common.StorageSize(len(node.external) * common.HashLength)
		}
	}
}

// flushItem is used to track all [cachedNode]s that must be written to disk
type flushItem struct {
	hash common.Hash
	node *cachedNode
	rlp  []byte
}

// writeFlushItems writes all items in [toFlush] to disk in batches of
// [ethdb.IdealBatchSize]. This function does not access any variables inside
// of [Database] and does not need to be synchronized.
func (db *Database) writeFlushItems(toFlush []*flushItem) error {
	batch := db.diskdb.NewBatch()
	for _, item := range toFlush {
		rlp := item.node.node
		item.rlp = rlp
		rawdb.WriteLegacyTrieNode(batch, item.hash, rlp)

		// If we exceeded the ideal batch size, commit and reset
		if batch.ValueSize() >= ethdb.IdealBatchSize {
			if err := batch.Write(); err != nil {
				log.Error("Failed to write flush list to disk", "err", err)
				return err
			}
			batch.Reset()
		}
	}

	// Flush out any remainder data from the last batch
	if err := batch.Write(); err != nil {
		log.Error("Failed to write flush list to disk", "err", err)
		return err
	}

	return nil
}

// Cap iteratively flushes old but still referenced trie nodes until the total
// memory usage goes below the given threshold.
func (db *Database) Cap(limit common.StorageSize) error {
	start := time.Now()

	// It is important that outside code doesn't see an inconsistent state
	// (referenced data removed from memory cache during commit but not yet
	// in persistent storage). This is ensured by only uncaching existing
	// data when the database write finalizes.
	db.lock.RLock()
	lockStart := time.Now()
	nodes, storage := len(db.dirties), db.dirtiesSize

	// db.dirtiesSize only contains the useful data in the cache, but when reporting
	// the total memory consumption, the maintenance metadata is also needed to be
	// counted.
	pendingSize := db.dirtiesSize + common.StorageSize(len(db.dirties)*cachedNodeSize)
	pendingSize += db.childrenSize
	if pendingSize <= limit {
		db.lock.RUnlock()
		return nil
	}

	// Keep removing nodes from the flush-list until we're below allowance
	toFlush := make([]*flushItem, 0, 128)
	oldest := db.oldest
	for pendingSize > limit && oldest != (common.Hash{}) {
		// Fetch the oldest referenced node and push into the batch
		node := db.dirties[oldest]
		toFlush = append(toFlush, &flushItem{oldest, node, nil})

		// Iterate to the next flush item, or abort if the size cap was achieved. Size
		// is the total size, including the useful cached data (hash -> blob), the
		// cache item metadata, as well as external children mappings.
		pendingSize -= common.StorageSize(common.HashLength + len(node.node) + cachedNodeSize)
		if node.external != nil {
			pendingSize -= common.StorageSize(len(node.external) * common.HashLength)
		}
		oldest = node.flushNext
	}
	db.lock.RUnlock()
	lockTime := time.Since(lockStart)

	// Write nodes to disk
	if err := db.writeFlushItems(toFlush); err != nil {
		return err
	}

	// Flush all written items from dirites
	//
	// NOTE: The order of the flushlist may have changed while the lock was not
	// held, so we cannot just iterate to [oldest].
	db.lock.Lock()
	defer db.lock.Unlock()
	lockStart = time.Now()
	for _, item := range toFlush {
		// [item.rlp] is populated in [writeFlushItems]
		db.removeFromDirties(item.hash, item.rlp)
	}
	db.flushnodes += uint64(nodes - len(db.dirties))
	db.flushsize += storage - db.dirtiesSize
	db.flushtime += time.Since(start)

	memcacheDirtySizeGauge.Update(float64(db.dirtiesSize))
	memcacheDirtyChildSizeGauge.Update(float64(db.childrenSize))
	memcacheDirtyNodesGauge.Update(int64(len(db.dirties)))

	memcacheFlushMeter.Mark(1)
	memcacheFlushTimeTimer.Update(time.Since(start))
	memcacheFlushLockTimeTimer.Update(lockTime + time.Since(lockStart))
	memcacheFlushSizeMeter.Mark(int64(storage - db.dirtiesSize))
	memcacheFlushNodesMeter.Mark(int64(nodes - len(db.dirties)))

	log.Debug("Persisted nodes from memory database", "nodes", nodes-len(db.dirties), "size", storage-db.dirtiesSize, "time", time.Since(start),
		"flushnodes", db.flushnodes, "flushsize", db.flushsize, "flushtime", db.flushtime, "livenodes", len(db.dirties), "livesize", db.dirtiesSize)
	return nil
}

// Commit iterates over all the children of a particular node, writes them out
// to disk, forcefully tearing down all references in both directions. As a side
// effect, all pre-images accumulated up to this point are also written.
func (db *Database) Commit(node common.Hash, report bool) error {
	start := time.Now()

	// It is important that outside code doesn't see an inconsistent state (referenced
	// data removed from memory cache during commit but not yet in persistent storage).
	// This is ensured by only uncaching existing data when the database write finalizes.
	db.lock.RLock()
	lockStart := time.Now()
	nodes, storage := len(db.dirties), db.dirtiesSize
	toFlush, err := db.commit(node, make([]*flushItem, 0, 128))
	if err != nil {
		db.lock.RUnlock()
		log.Error("Failed to commit trie from trie database", "err", err)
		return err
	}
	db.lock.RUnlock()
	lockTime := time.Since(lockStart)

	// Write nodes to disk
	if err := db.writeFlushItems(toFlush); err != nil {
		return err
	}

	// Flush all written items from dirites
	db.lock.Lock()
	defer db.lock.Unlock()
	lockStart = time.Now()
	for _, item := range toFlush {
		// [item.rlp] is populated in [writeFlushItems]
		db.removeFromDirties(item.hash, item.rlp)
	}

	memcacheDirtySizeGauge.Update(float64(db.dirtiesSize))
	memcacheDirtyChildSizeGauge.Update(float64(db.childrenSize))
	memcacheDirtyNodesGauge.Update(int64(len(db.dirties)))

	memcacheCommitMeter.Mark(1)
	memcacheCommitTimeTimer.Update(time.Since(start))
	memcacheCommitLockTimeTimer.Update(lockTime + time.Since(lockStart))
	memcacheCommitSizeMeter.Mark(int64(storage - db.dirtiesSize))
	memcacheCommitNodesMeter.Mark(int64(nodes - len(db.dirties)))

	logger := log.Info
	if !report {
		logger = log.Debug
	}
	logger("Persisted trie from memory database", "nodes", nodes-len(db.dirties)+int(db.flushnodes), "size", storage-db.dirtiesSize+db.flushsize, "time", time.Since(start)+db.flushtime,
		"gcnodes", db.gcnodes, "gcsize", db.gcsize, "gctime", db.gctime, "livenodes", len(db.dirties), "livesize", db.dirtiesSize)

	// Reset the garbage collection statistics
	db.gcnodes, db.gcsize, db.gctime = 0, 0, 0
	db.flushnodes, db.flushsize, db.flushtime = 0, 0, 0
	return nil
}

// commit is the private locked version of Commit. This function does not
// mutate any data, rather it collects all data that should be committed.
//
// [callback] will be invoked as soon as it is determined a trie node will be
// flushed to disk (before it is actually written).
func (db *Database) commit(hash common.Hash, toFlush []*flushItem) ([]*flushItem, error) {
	// If the node does not exist, it's a previously committed node
	node, ok := db.dirties[hash]
	if !ok {
		return toFlush, nil
	}
	var err error
	node.forChildren(db.resolver, func(child common.Hash) {
		if err == nil {
			toFlush, err = db.commit(child, toFlush)
		}
	})
	if err != nil {
		return nil, err
	}
	// By processing the children of each node before the node itself, we ensure
	// that children are committed before their parents (an invariant of this
	// package).
	toFlush = append(toFlush, &flushItem{hash, node, nil})
	return toFlush, nil
}

// removeFromDirties is invoked after database writes and implements dirty data uncaching.
//
// This is the post-processing step of a commit operation where the already persisted trie is
// removed from the dirty cache and moved into the clean cache. The reason behind
// the two-phase commit is to ensure data availability while moving from memory
// to disk.
//
// It is assumed the caller holds the [dirtiesLock] when this function is
// called.
func (db *Database) removeFromDirties(hash common.Hash, rlp []byte) {
	// If the node does not exist, we're done on this path. This could happen if
	// nodes are capped to disk while another thread is committing those same
	// nodes.
	node, ok := db.dirties[hash]
	if !ok {
		return
	}
	// Node still exists, remove it from the flush-list
	switch hash {
	case db.oldest:
		db.oldest = node.flushNext
		if node.flushNext != (common.Hash{}) {
			db.dirties[node.flushNext].flushPrev = common.Hash{}
		}
	case db.newest:
		db.newest = node.flushPrev
		if node.flushPrev != (common.Hash{}) {
			db.dirties[node.flushPrev].flushNext = common.Hash{}
		}
	default:
		db.dirties[node.flushPrev].flushNext = node.flushNext
		db.dirties[node.flushNext].flushPrev = node.flushPrev
	}
	// Remove the node from the dirty cache
	delete(db.dirties, hash)
	db.dirtiesSize -= common.StorageSize(common.HashLength + len(node.node))
	if node.external != nil {
		db.childrenSize -= common.StorageSize(len(node.external) * common.HashLength)
	}
	// Move the flushed node into the clean cache to prevent insta-reloads
	if db.cleans != nil {
		db.cleans.Set(hash[:], rlp)
		memcacheCleanWriteMeter.Mark(int64(len(rlp)))
	}
}

// Initialized returns an indicator if state data is already initialized
// in hash-based scheme by checking the presence of genesis state.
func (db *Database) Initialized(genesisRoot common.Hash) bool {
	return rawdb.HasLegacyTrieNode(db.diskdb, genesisRoot)
}

// Update inserts the dirty nodes in provided nodeset into database and link the
// account trie with multiple storage tries if necessary.
func (db *Database) Update(root common.Hash, parent common.Hash, nodes *trienode.MergedNodeSet) error {
	// Ensure the parent state is present and signal a warning if not.
	if parent != types.EmptyRootHash {
		if blob, _ := db.Node(parent); len(blob) == 0 {
			log.Error("parent state is not present")
		}
	}
	db.lock.Lock()
	defer db.lock.Unlock()

	return db.update(root, parent, nodes)
}

// UpdateAndReferenceRoot inserts the dirty nodes in provided nodeset into
// database and links the account trie with multiple storage tries if necessary,
// then adds a reference [from] root to the metaroot while holding the db's lock.
func (db *Database) UpdateAndReferenceRoot(root common.Hash, parent common.Hash, nodes *trienode.MergedNodeSet) error {
	// Ensure the parent state is present and signal a warning if not.
	if parent != types.EmptyRootHash {
		if blob, _ := db.Node(parent); len(blob) == 0 {
			log.Error("parent state is not present")
		}
	}
	db.lock.Lock()
	defer db.lock.Unlock()

	if err := db.update(root, parent, nodes); err != nil {
		return err
	}
	db.reference(root, common.Hash{})
	return nil
}

func (db *Database) update(root common.Hash, parent common.Hash, nodes *trienode.MergedNodeSet) error {
	// Insert dirty nodes into the database. In the same tree, it must be
	// ensured that children are inserted first, then parent so that children
	// can be linked with their parent correctly.
	//
	// Note, the storage tries must be flushed before the account trie to
	// retain the invariant that children go into the dirty cache first.
	var order []common.Hash
	for owner := range nodes.Sets {
		if owner == (common.Hash{}) {
			continue
		}
		order = append(order, owner)
	}
	if _, ok := nodes.Sets[common.Hash{}]; ok {
		order = append(order, common.Hash{})
	}
	for _, owner := range order {
		subset := nodes.Sets[owner]
		subset.ForEachWithOrder(func(path string, n *trienode.Node) {
			if n.IsDeleted() {
				return // ignore deletion
			}
			db.insert(n.Hash, n.Blob)
		})
	}
	// Link up the account trie and storage trie if the node points
	// to an account trie leaf.
	if set, present := nodes.Sets[common.Hash{}]; present {
		for _, n := range set.Leaves {
			var account types.StateAccount
			if err := rlp.DecodeBytes(n.Blob, &account); err != nil {
				return err
			}
			if account.Root != types.EmptyRootHash {
				db.reference(account.Root, n.Parent)
			}
		}
	}
	return nil
}

// Size returns the current storage size of the memory cache in front of the
// persistent database layer.
func (db *Database) Size() common.StorageSize {
	db.lock.RLock()
	defer db.lock.RUnlock()

	// db.dirtiesSize only contains the useful data in the cache, but when reporting
	// the total memory consumption, the maintenance metadata is also needed to be
	// counted.
	var metadataSize = common.StorageSize(len(db.dirties) * cachedNodeSize)
	return db.dirtiesSize + db.childrenSize + metadataSize
}

// Close closes the trie database and releases all held resources.
func (db *Database) Close() error { return nil }

// Scheme returns the node scheme used in the database.
func (db *Database) Scheme() string {
	return rawdb.HashScheme
}

// Reader retrieves a node reader belonging to the given state root.
func (db *Database) Reader(root common.Hash) *reader {
	return &reader{db: db}
}

// reader is a state reader of Database which implements the Reader interface.
type reader struct {
	db *Database
}

// Node retrieves the trie node with the given node hash.
// No error will be returned if the node is not found.
func (reader *reader) Node(owner common.Hash, path []byte, hash common.Hash) ([]byte, error) {
	blob, _ := reader.db.Node(hash)
	return blob, nil
}
