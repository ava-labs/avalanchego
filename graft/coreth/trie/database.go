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

package trie

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/metrics"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	defaultPreimagesLimit = 4 * 1024 * 1024 // 4 MB
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

// Database is an intermediate write layer between the trie data structures and
// the disk database. The aim is to accumulate trie writes in-memory and only
// periodically flush a couple tries to disk, garbage collecting the remainder.
//
// The trie Database is thread-safe in its mutations and is thread-safe in providing individual,
// independent node access.
type Database struct {
	diskdb ethdb.KeyValueStore // Persistent storage for matured trie nodes

	preimagesLock sync.RWMutex           // Used to gate acess to [preimagesSize] and [preimages]
	preimagesSize common.StorageSize     // Storage size of the preimages cache
	preimages     map[common.Hash][]byte // Preimages of nodes from the secure trie

	dirtiesLock sync.RWMutex                // Used to gate access to all trie node data structures and metrics (everything below)
	cleans      *fastcache.Cache            // GC friendly memory cache of clean node RLPs
	dirties     map[common.Hash]*cachedNode // Data and references relationships of dirty trie nodes
	oldest      common.Hash                 // Oldest tracked node, flush-list head
	newest      common.Hash                 // Newest tracked node, flush-list tail

	gctime  time.Duration      // Time spent on garbage collection since last commit
	gcnodes uint64             // Nodes garbage collected since last commit
	gcsize  common.StorageSize // Data storage garbage collected since last commit

	flushtime  time.Duration      // Time spent on data flushing since last commit
	flushnodes uint64             // Nodes flushed since last commit
	flushsize  common.StorageSize // Data storage flushed since last commit

	dirtiesSize  common.StorageSize // Storage size of the dirty node cache (exc. metadata)
	childrenSize common.StorageSize // Storage size of the external children tracking
}

// rawNode is a simple binary blob used to differentiate between collapsed trie
// nodes and already encoded RLP binary blobs (while at the same time store them
// in the same cache fields).
type rawNode []byte

func (n rawNode) cache() (hashNode, bool)   { panic("this should never end up in a live trie") }
func (n rawNode) fstring(ind string) string { panic("this should never end up in a live trie") }

func (n rawNode) EncodeRLP(w io.Writer) error {
	_, err := w.Write(n)
	return err
}

// rawFullNode represents only the useful data content of a full node, with the
// caches and flags stripped out to minimize its data storage. This type honors
// the same RLP encoding as the original parent.
type rawFullNode [17]node

func (n rawFullNode) cache() (hashNode, bool)   { panic("this should never end up in a live trie") }
func (n rawFullNode) fstring(ind string) string { panic("this should never end up in a live trie") }

func (n rawFullNode) EncodeRLP(w io.Writer) error {
	eb := rlp.NewEncoderBuffer(w)
	n.encode(eb)
	return eb.Flush()
}

// rawShortNode represents only the useful data content of a short node, with the
// caches and flags stripped out to minimize its data storage. This type honors
// the same RLP encoding as the original parent.
type rawShortNode struct {
	Key []byte
	Val node
}

func (n rawShortNode) cache() (hashNode, bool)   { panic("this should never end up in a live trie") }
func (n rawShortNode) fstring(ind string) string { panic("this should never end up in a live trie") }

// cachedNode is all the information we know about a single cached trie node
// in the memory database write layer.
type cachedNode struct {
	node node   // Cached collapsed trie node, or raw rlp data
	size uint16 // Byte size of the useful cached data

	parents  uint32                 // Number of live nodes referencing this one
	children map[common.Hash]uint16 // External children referenced by this node

	flushPrev common.Hash // Previous node in the flush-list
	flushNext common.Hash // Next node in the flush-list
}

// cachedNodeSize is the raw size of a cachedNode data structure without any
// node data included. It's an approximate size, but should be a lot better
// than not counting them.
var cachedNodeSize = int(reflect.TypeOf(cachedNode{}).Size())

// cachedNodeChildrenSize is the raw size of an initialized but empty external
// reference map.
const cachedNodeChildrenSize = 48

// rlp returns the raw rlp encoded blob of the cached trie node, either directly
// from the cache, or by regenerating it from the collapsed node.
func (n *cachedNode) rlp() []byte {
	if node, ok := n.node.(rawNode); ok {
		return node
	}
	return nodeToBytes(n.node)
}

// obj returns the decoded and expanded trie node, either directly from the cache,
// or by regenerating it from the rlp encoded blob.
func (n *cachedNode) obj(hash common.Hash) node {
	if node, ok := n.node.(rawNode); ok {
		return mustDecodeNode(hash[:], node)
	}
	return expandNode(hash[:], n.node)
}

// forChilds invokes the callback for all the tracked children of this node,
// both the implicit ones from inside the node as well as the explicit ones
// from outside the node.
func (n *cachedNode) forChilds(onChild func(hash common.Hash)) {
	for child := range n.children {
		onChild(child)
	}
	if _, ok := n.node.(rawNode); !ok {
		forGatherChildren(n.node, onChild)
	}
}

// forGatherChildren traverses the node hierarchy of a collapsed storage node and
// invokes the callback for all the hashnode children.
func forGatherChildren(n node, onChild func(hash common.Hash)) {
	switch n := n.(type) {
	case *rawShortNode:
		forGatherChildren(n.Val, onChild)
	case rawFullNode:
		for i := 0; i < 16; i++ {
			forGatherChildren(n[i], onChild)
		}
	case hashNode:
		onChild(common.BytesToHash(n))
	case valueNode, nil, rawNode:
	default:
		panic(fmt.Sprintf("unknown node type: %T", n))
	}
}

// simplifyNode traverses the hierarchy of an expanded memory node and discards
// all the internal caches, returning a node that only contains the raw data.
func simplifyNode(n node) node {
	switch n := n.(type) {
	case *shortNode:
		// Short nodes discard the flags and cascade
		return &rawShortNode{Key: n.Key, Val: simplifyNode(n.Val)}

	case *fullNode:
		// Full nodes discard the flags and cascade
		node := rawFullNode(n.Children)
		for i := 0; i < len(node); i++ {
			if node[i] != nil {
				node[i] = simplifyNode(node[i])
			}
		}
		return node

	case valueNode, hashNode, rawNode:
		return n

	default:
		panic(fmt.Sprintf("unknown node type: %T", n))
	}
}

// expandNode traverses the node hierarchy of a collapsed storage node and converts
// all fields and keys into expanded memory form.
func expandNode(hash hashNode, n node) node {
	switch n := n.(type) {
	case *rawShortNode:
		// Short nodes need key and child expansion
		return &shortNode{
			Key: compactToHex(n.Key),
			Val: expandNode(nil, n.Val),
			flags: nodeFlag{
				hash: hash,
			},
		}

	case rawFullNode:
		// Full nodes need child expansion
		node := &fullNode{
			flags: nodeFlag{
				hash: hash,
			},
		}
		for i := 0; i < len(node.Children); i++ {
			if n[i] != nil {
				node.Children[i] = expandNode(nil, n[i])
			}
		}
		return node

	case valueNode, hashNode:
		return n

	default:
		panic(fmt.Sprintf("unknown node type: %T", n))
	}
}

// Config defines all necessary options for database.
type Config struct {
	Cache     int  // Memory allowance (MB) to use for caching trie nodes in memory
	Preimages bool // Flag whether the preimage of trie key is recorded
}

// NewDatabase creates a new trie database to store ephemeral trie content before
// its written out to disk or garbage collected. No read cache is created, so all
// data retrievals will hit the underlying disk database.
func NewDatabase(diskdb ethdb.KeyValueStore) *Database {
	return NewDatabaseWithConfig(diskdb, nil)
}

// NewDatabaseWithConfig creates a new trie database to store ephemeral trie content
// before its written out to disk or garbage collected. It also acts as a read cache
// for nodes loaded from disk.
func NewDatabaseWithConfig(diskdb ethdb.KeyValueStore, config *Config) *Database {
	var cleans *fastcache.Cache
	if config != nil && config.Cache > 0 {
		cleans = fastcache.New(config.Cache * 1024 * 1024)
	}
	db := &Database{
		diskdb: diskdb,
		cleans: cleans,
		dirties: map[common.Hash]*cachedNode{{}: {
			children: make(map[common.Hash]uint16),
		}},
	}
	if config == nil || config.Preimages { // TODO(karalabe): Flip to default off in the future
		db.preimages = make(map[common.Hash][]byte)
	}
	return db
}

// DiskDB retrieves the persistent storage backing the trie database.
func (db *Database) DiskDB() ethdb.KeyValueStore {
	return db.diskdb
}

// insert inserts a collapsed trie node into the memory database.
// The blob size must be specified to allow proper size tracking.
// All nodes inserted by this function will be reference tracked
// and in theory should only used for **trie nodes** insertion.
// insert assumes that the dirtiesLock is held by the caller.
func (db *Database) insert(hash common.Hash, size int, node node) {
	// If the node's already cached, skip
	if _, ok := db.dirties[hash]; ok {
		return
	}
	memcacheDirtyWriteMeter.Mark(int64(size))

	// Create the cached entry for this node
	entry := &cachedNode{
		node:      simplifyNode(node),
		size:      uint16(size),
		flushPrev: db.newest,
	}
	entry.forChilds(func(child common.Hash) {
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
	db.dirtiesSize += common.StorageSize(common.HashLength + entry.size)
}

// InsertPreimages writes a map of new trie node preimages to the
// memory database if it's yet unknown.
//
// The method will NOT make a copy of the provided slice,
// only use if the preimage will NOT be changed later on.
func (db *Database) InsertPreimages(preimages map[string][]byte) {
	// Short circuit if preimage collection is disabled
	if db.preimages == nil {
		return
	}
	// Track the preimage if a yet unknown one
	db.preimagesLock.Lock()
	defer db.preimagesLock.Unlock()
	for hk, preimage := range preimages {
		hash := common.BytesToHash([]byte(hk))
		if _, ok := db.preimages[hash]; ok {
			continue
		}
		db.preimages[hash] = preimage
		db.preimagesSize += common.StorageSize(common.HashLength + len(preimage))
	}
}

// Preimage retrieves a cached trie node pre-image from memory. If it cannot be
// found cached, the method queries the persistent database for the content.
func (db *Database) Preimage(hash common.Hash) []byte {
	// Short circuit if preimage collection is disabled
	if db.preimages == nil {
		return nil
	}
	// Retrieve the node from cache if available
	db.preimagesLock.RLock()
	preimage := db.preimages[hash]
	db.preimagesLock.RUnlock()
	if preimage != nil {
		return preimage
	}
	return rawdb.ReadPreimage(db.diskdb, hash)
}

// RawNode retrieves an encoded cached trie node from memory. If it cannot be found
// cached, the method queries the persistent database for the content. This function
// will not return the metaroot.
func (db *Database) RawNode(h common.Hash) ([]byte, error) {
	if h == (common.Hash{}) {
		return nil, errors.New("not found")
	}
	enc, cn, err := db.node(h)
	if err != nil {
		return nil, err
	}
	if len(enc) > 0 {
		return enc, nil
	}
	return cn.rlp(), nil
}

// EncodedNode returns a formatted [node] when given a node hash. If no node
// exists, nil is returned. This function will return the metaroot.
func (db *Database) EncodedNode(h common.Hash) node {
	enc, cn, err := db.node(h)
	if err != nil {
		return nil
	}
	if len(enc) > 0 {
		return mustDecodeNode(h[:], enc)
	}
	return cn.obj(h)
}

// node retrieves an encoded cached trie node from memory. If it cannot be found
// cached, the method queries the persistent database for the content.
//
// We do not return a single node representation to avoid useless
// encoding/decoding depending on the caller.
func (db *Database) node(hash common.Hash) ([]byte, *cachedNode, error) {
	// Retrieve the node from the clean cache if available
	if db.cleans != nil {
		if enc := db.cleans.Get(nil, hash[:]); enc != nil {
			memcacheCleanHitMeter.Mark(1)
			memcacheCleanReadMeter.Mark(int64(len(enc)))
			return enc, nil, nil
		}
	}
	// Retrieve the node from the dirty cache if available
	db.dirtiesLock.RLock()
	dirty := db.dirties[hash]
	db.dirtiesLock.RUnlock()

	if dirty != nil {
		memcacheDirtyHitMeter.Mark(1)
		memcacheDirtyReadMeter.Mark(int64(dirty.size))
		return nil, dirty, nil
	}
	memcacheDirtyMissMeter.Mark(1)

	// Content unavailable in memory, attempt to retrieve from disk
	enc := rawdb.ReadTrieNode(db.diskdb, hash)
	if len(enc) != 0 {
		if db.cleans != nil {
			db.cleans.Set(hash[:], enc)
			memcacheCleanMissMeter.Mark(1)
			memcacheCleanWriteMeter.Mark(int64(len(enc)))
		}
		return enc, nil, nil
	}
	return nil, nil, errors.New("not found")
}

// Nodes retrieves the hashes of all the nodes cached within the memory database.
// This method is extremely expensive and should only be used to validate internal
// states in test code.
func (db *Database) Nodes() []common.Hash {
	db.dirtiesLock.RLock()
	defer db.dirtiesLock.RUnlock()

	var hashes = make([]common.Hash, 0, len(db.dirties))
	for hash := range db.dirties {
		if hash != (common.Hash{}) { // Special case for "root" references/nodes
			hashes = append(hashes, hash)
		}
	}
	return hashes
}

// Reference adds a new reference from a parent node to a child node.
// This function is used to add reference between internal trie node
// and external node(e.g. storage trie root), all internal trie nodes
// are referenced together by database itself.
func (db *Database) Reference(child common.Hash, parent common.Hash, grabDirtyLock bool) {
	if grabDirtyLock {
		db.dirtiesLock.Lock()
		defer db.dirtiesLock.Unlock()
	}

	// If the node does not exist, it's a node pulled from disk, skip
	node, ok := db.dirties[child]
	if !ok {
		return
	}
	// If the reference already exists, only duplicate for roots
	if db.dirties[parent].children == nil {
		db.dirties[parent].children = make(map[common.Hash]uint16)
		db.childrenSize += cachedNodeChildrenSize
	} else if _, ok = db.dirties[parent].children[child]; ok && parent != (common.Hash{}) {
		return
	}
	node.parents++
	db.dirties[parent].children[child]++
	if db.dirties[parent].children[child] == 1 {
		db.childrenSize += common.HashLength + 2 // uint16 counter
	}
}

// Dereference removes an existing reference from a root node.
func (db *Database) Dereference(root common.Hash) {
	// Sanity check to ensure that the meta-root is not removed
	if root == (common.Hash{}) {
		log.Error("Attempted to dereference the trie cache meta root")
		return
	}

	db.dirtiesLock.Lock()
	defer db.dirtiesLock.Unlock()
	nodes, storage, start := len(db.dirties), db.dirtiesSize, time.Now()
	db.dereference(root, common.Hash{})

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
func (db *Database) dereference(child common.Hash, parent common.Hash) {
	// Dereference the parent-child
	node := db.dirties[parent]

	if node.children != nil && node.children[child] > 0 {
		node.children[child]--
		if node.children[child] == 0 {
			delete(node.children, child)
			db.childrenSize -= (common.HashLength + 2) // uint16 counter
		}
	}
	// If the child does not exist, it's a previously committed node.
	node, ok := db.dirties[child]
	if !ok {
		return
	}
	// If there are no more references to the child, delete it and cascade
	if node.parents > 0 {
		// This is a special cornercase where a node loaded from disk (i.e. not in the
		// memcache any more) gets reinjected as a new node (short node split into full,
		// then reverted into short), causing a cached node to have no parents. That is
		// no problem in itself, but don't make maxint parents out of it.
		node.parents--
	}
	if node.parents == 0 {
		// Remove the node from the flush-list
		switch child {
		case db.oldest:
			db.oldest = node.flushNext
			db.dirties[node.flushNext].flushPrev = common.Hash{}
		case db.newest:
			db.newest = node.flushPrev
			db.dirties[node.flushPrev].flushNext = common.Hash{}
		default:
			db.dirties[node.flushPrev].flushNext = node.flushNext
			db.dirties[node.flushNext].flushPrev = node.flushPrev
		}
		// Dereference all children and delete the node
		node.forChilds(func(hash common.Hash) {
			db.dereference(hash, child)
		})
		delete(db.dirties, child)
		db.dirtiesSize -= common.StorageSize(common.HashLength + int(node.size))
		if node.children != nil {
			db.childrenSize -= cachedNodeChildrenSize
		}
	}
}

// WritePreimages writes all preimages to disk if more than [limit] are currently in
// memory.
func (db *Database) WritePreimages(limit common.StorageSize) error {
	// Short circuit if preimage collection is disabled
	if db.preimages == nil {
		return nil
	}

	// If the preimage cache got large enough, push to disk. If it's still small
	// leave for later to deduplicate writes.
	db.preimagesLock.RLock()
	if db.preimagesSize <= limit {
		db.preimagesLock.RUnlock()
		return nil
	}
	toFlush := make(map[common.Hash][]byte)
	for k, v := range db.preimages {
		toFlush[k] = v
	}
	db.preimagesLock.RUnlock()

	// Short circuit if nothing to do
	if len(toFlush) == 0 {
		return nil
	}

	// Write preimages to disk
	batch := db.diskdb.NewBatch()
	rawdb.WritePreimages(batch, toFlush)
	if err := batch.Write(); err != nil {
		return err
	}

	// Write successful, clear out the flushed data
	db.preimagesLock.Lock()
	defer db.preimagesLock.Unlock()
	for hash, preimage := range toFlush {
		delete(db.preimages, hash)
		db.preimagesSize -= common.StorageSize(common.HashLength + len(preimage))
	}
	return nil
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
func (db *Database) writeFlushItems(toFlush []flushItem) error {
	batch := db.diskdb.NewBatch()
	for _, item := range toFlush {
		rlp := item.node.rlp()
		item.rlp = rlp
		rawdb.WriteTrieNode(batch, item.hash, rlp)

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
	if err := db.WritePreimages(defaultPreimagesLimit); err != nil {
		return err
	}

	// It is important that outside code doesn't see an inconsistent state
	// (referenced data removed from memory cache during commit but not yet
	// in persistent storage). This is ensured by only uncaching existing
	// data when the database write finalizes.
	db.dirtiesLock.RLock()
	lockStart := time.Now()
	nodes, storage := len(db.dirties), db.dirtiesSize

	// db.dirtiesSize only contains the useful data in the cache, but when reporting
	// the total memory consumption, the maintenance metadata is also needed to be
	// counted.
	pendingSize := db.dirtiesSize + common.StorageSize((len(db.dirties)-1)*cachedNodeSize)
	pendingSize += db.childrenSize - common.StorageSize(len(db.dirties[common.Hash{}].children)*(common.HashLength+2))
	if pendingSize <= limit {
		db.dirtiesLock.RUnlock()
		return nil
	}

	// Keep removing nodes from the flush-list until we're below allowance
	toFlush := make([]flushItem, 0, 128)
	oldest := db.oldest
	for pendingSize > limit && oldest != (common.Hash{}) {
		// Fetch the oldest referenced node and push into the batch
		node := db.dirties[oldest]
		toFlush = append(toFlush, flushItem{oldest, node, nil})

		// Iterate to the next flush item, or abort if the size cap was achieved. Size
		// is the total size, including the useful cached data (hash -> blob), the
		// cache item metadata, as well as external children mappings.
		pendingSize -= common.StorageSize(common.HashLength + int(node.size) + cachedNodeSize)
		if node.children != nil {
			pendingSize -= common.StorageSize(cachedNodeChildrenSize + len(node.children)*(common.HashLength+2))
		}
		oldest = node.flushNext
	}
	db.dirtiesLock.RUnlock()
	lockTime := time.Since(lockStart)

	// Write nodes to disk
	if err := db.writeFlushItems(toFlush); err != nil {
		return err
	}

	// Flush all written items from dirites
	//
	// NOTE: The order of the flushlist may have changed while the lock was not
	// held, so we cannot just iterate to [oldest].
	db.dirtiesLock.Lock()
	defer db.dirtiesLock.Unlock()
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
func (db *Database) Commit(node common.Hash, report bool, callback func(common.Hash)) error {
	start := time.Now()
	if err := db.WritePreimages(0); err != nil {
		return err
	}

	// It is important that outside code doesn't see an inconsistent state (referenced
	// data removed from memory cache during commit but not yet in persistent storage).
	// This is ensured by only uncaching existing data when the database write finalizes.
	db.dirtiesLock.RLock()
	lockStart := time.Now()
	nodes, storage := len(db.dirties), db.dirtiesSize
	toFlush, err := db.commit(node, make([]flushItem, 0, 128), callback)
	if err != nil {
		log.Error("Failed to commit trie from trie database", "err", err)
		return err
	}
	db.dirtiesLock.RUnlock()
	lockTime := time.Since(lockStart)

	// Write nodes to disk
	if err := db.writeFlushItems(toFlush); err != nil {
		return err
	}

	// Flush all written items from dirites
	db.dirtiesLock.Lock()
	defer db.dirtiesLock.Unlock()
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
func (db *Database) commit(hash common.Hash, toFlush []flushItem, callback func(common.Hash)) ([]flushItem, error) {
	// If the node does not exist, it's a previously committed node
	node, ok := db.dirties[hash]
	if !ok {
		return toFlush, nil
	}
	var err error
	node.forChilds(func(child common.Hash) {
		if err == nil {
			toFlush, err = db.commit(child, toFlush, callback)
		}
	})
	if err != nil {
		return nil, err
	}
	// By processing the children of each node before the node itself, we ensure
	// that children are committed before their parents (an invariant of this
	// package).
	toFlush = append(toFlush, flushItem{hash, node, nil})
	if callback != nil {
		callback(hash)
	}
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
		db.dirties[node.flushNext].flushPrev = common.Hash{}
	case db.newest:
		db.newest = node.flushPrev
		db.dirties[node.flushPrev].flushNext = common.Hash{}
	default:
		db.dirties[node.flushPrev].flushNext = node.flushNext
		db.dirties[node.flushNext].flushPrev = node.flushPrev
	}
	// Remove the node from the dirty cache
	delete(db.dirties, hash)
	db.dirtiesSize -= common.StorageSize(common.HashLength + int(node.size))
	if node.children != nil {
		db.childrenSize -= common.StorageSize(cachedNodeChildrenSize + len(node.children)*(common.HashLength+2))
	}
	// Move the flushed node into the clean cache to prevent insta-reloads
	if db.cleans != nil {
		db.cleans.Set(hash[:], rlp)
		memcacheCleanWriteMeter.Mark(int64(len(rlp)))
	}
}

// Size returns the current storage size of the memory cache in front of the
// persistent database layer.
func (db *Database) Size() (common.StorageSize, common.StorageSize) {
	db.preimagesLock.RLock()
	preimagesSize := db.preimagesSize
	db.preimagesLock.RUnlock()

	// db.dirtiesSize only contains the useful data in the cache, but when reporting
	// the total memory consumption, the maintenance metadata is also needed to be
	// counted.
	db.dirtiesLock.RLock()
	defer db.dirtiesLock.RUnlock()
	var metadataSize = common.StorageSize((len(db.dirties) - 1) * cachedNodeSize)
	var metarootRefs = common.StorageSize(len(db.dirties[common.Hash{}].children) * (common.HashLength + 2))
	return db.dirtiesSize + db.childrenSize + metadataSize - metarootRefs, preimagesSize
}
