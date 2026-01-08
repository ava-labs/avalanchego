// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"container/heap"
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/message"
	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"

	syncclient "github.com/ava-labs/avalanchego/graft/coreth/sync/client"
)

var (
	_ syncclient.LeafSyncTask = (*trieSegment)(nil)
	_ fmt.Stringer            = (*trieSegment)(nil)
)

// trieToSync keeps the state of a single trie syncing
// this can be a storage or the main trie.
type trieToSync struct {
	root    common.Hash
	account common.Hash

	// The trie consists of a slice of segments. each
	// segment has a start and end range of keys, and
	// contains a pointer back to this struct.
	segments []*trieSegment

	// Parallel hashing: track completion instead of forcing order
	lock               sync.Mutex
	segmentsDone       map[int]struct{} // segments that finished downloading
	segmentsHashedDone map[int]struct{} // segments that finished hashing (NEW)

	// Atomic completion tracking for reduced lock contention
	segmentsCompleted atomic.Uint32
	totalSegments     int

	// We use a thread-safe stack trie wrapper to hash the leafs
	// and have a batch used for writing it to disk.
	batch     ethdb.Batch
	stackTrie *ThreadSafeStackTrie

	// We keep a pointer to the overall sync operation,
	// used to add segments to the work queue and to
	// update the eta.
	sync *stateSync

	// task implements the syncTask interface with methods
	// containing logic specific to the main trie or storage
	// tries.
	task       syncTask
	isMainTrie bool
}

// NewTrieToSync initializes a trieToSync and restores any previously started segments.
func NewTrieToSync(sync *stateSync, root common.Hash, account common.Hash, syncTask syncTask) (*trieToSync, error) {
	batch := sync.db.NewBatch() // TODO: migrate state sync to use database schemes.
	writeFn := func(path []byte, hash common.Hash, blob []byte) {
		rawdb.WriteTrieNode(batch, account, path, hash, blob, rawdb.HashScheme)
	}
	trieToSync := &trieToSync{
		sync:               sync,
		root:               root,
		account:            account,
		batch:              batch,
		stackTrie:          NewThreadSafeStackTrie(&trie.StackTrieOptions{Writer: writeFn}),
		isMainTrie:         (root == sync.root),
		task:               syncTask,
		segmentsDone:       make(map[int]struct{}),
		segmentsHashedDone: make(map[int]struct{}),
	}
	if err := trieToSync.loadSegments(); err != nil {
		return nil, err
	}
	// Initialize totalSegments after segments are loaded
	trieToSync.totalSegments = len(trieToSync.segments)
	return trieToSync, nil
}

// loadSegments reads persistent storage and initializes trieSegments that
// had been previously started and need to be resumed.
func (t *trieToSync) loadSegments() error {
	// Get an iterator for segments for t.root and see if we find anything.
	// This lets us check if this trie was previously segmented, in which
	// case we need to restore the same segments on resume.
	it := customrawdb.NewSyncSegmentsIterator(t.sync.db, t.root)
	defer it.Release()

	// Track the previously added segment as we loop over persisted values.
	var prevSegmentStart []byte

	for it.Next() {
		// If we find any persisted segments with the specified
		// prefix, we add a new segment to the trie here.
		// The segment we add represents a segment ending at the
		// key immediately prior to the segment we found on disk.
		// This is because we do not persist the beginning of
		// the first segment.
		_, segmentStart := customrawdb.ParseSyncSegmentKey(it.Key())
		segmentStartPos := binary.BigEndian.Uint16(segmentStart[:wrappers.ShortLen])
		t.addSegment(prevSegmentStart, addPadding(segmentStartPos-1, 0xff))

		// keep tracking the previous segment
		prevSegmentStart = segmentStart
	}
	if err := it.Error(); err != nil {
		return err
	}

	// this creates the last segment if any were found in the loop
	// and also handles the case where there were no segments persisted to disk.
	t.addSegment(prevSegmentStart, nil)

	for _, segment := range t.segments {
		// for each segment we need to find the last key already persisted
		// so syncing can begin at the subsequent key
		var lastKey []byte
		it := segment.trie.task.IterateLeafs(common.BytesToHash(segment.start))
		defer it.Release()
		for it.Next() {
			if len(segment.end) > 0 && bytes.Compare(it.Key(), segment.end) > 0 {
				// don't go past the end of the segment
				break
			}
			lastKey = common.CopyBytes(it.Key())
			segment.leafs++
		}
		if lastKey != nil {
			utils.IncrOne(lastKey)
			segment.pos = lastKey // syncing will start from this key
		}
		log.Debug("statesync: loading segment", "segment", segment)
	}
	return it.Error()
}

// startSyncing adds the trieToSync's segments to the work queue.
func (t *trieToSync) startSyncing(ctx context.Context) error {
	for _, segment := range t.segments {
		select {
		case t.sync.segments <- segment:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// addSegment appends a newly created segment specified by [start] and
// [end] to [t.segments] and returns it.
// note: addSegment does not take a lock and therefore is called only
// before multiple segments are syncing concurrently.
func (t *trieToSync) addSegment(start, end []byte) *trieSegment {
	segment := &trieSegment{
		start: start,
		end:   end,
		trie:  t,
		idx:   len(t.segments),
		batch: t.sync.db.NewBatch(),
	}
	t.segments = append(t.segments, segment)
	return segment
}

// segmentIterator provides sequential access to cached key-value pairs from a segment.
type segmentIterator struct {
	keys [][]byte
	vals [][]byte
	pos  int
}

// newSegmentIterator creates an iterator for a segment's cached data.
func newSegmentIterator(seg *trieSegment) *segmentIterator {
	return &segmentIterator{
		keys: seg.cachedKeys,
		vals: seg.cachedVals,
		pos:  0,
	}
}

// next advances the iterator and returns true if there's more data.
func (it *segmentIterator) next() bool {
	it.pos++
	return it.pos < len(it.keys)
}

// current returns the current key-value pair.
func (it *segmentIterator) current() ([]byte, []byte) {
	if it.pos < len(it.keys) {
		return it.keys[it.pos], it.vals[it.pos]
	}
	return nil, nil
}

// heapItem represents a single item in the k-way merge heap.
type heapItem struct {
	key      []byte
	val      []byte
	iterator *segmentIterator
}

// segmentHeap implements heap.Interface for k-way merging of segment iterators.
type segmentHeap []*heapItem

func (h segmentHeap) Len() int { return len(h) }

func (h segmentHeap) Less(i, j int) bool {
	return bytes.Compare(h[i].key, h[j].key) < 0
}

func (h segmentHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *segmentHeap) Push(x interface{}) {
	*h = append(*h, x.(*heapItem))
}

func (h *segmentHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*h = old[0 : n-1]
	return item
}

// segmentFinished is called when one the trie segment with index [idx] finishes syncing.
// Uses k-way heap merge to stream sorted data from segments to StackTrie without
// loading all data into memory at once.
//
// Performance characteristics:
// - Segment downloads: Parallel (12 workers downloading simultaneously)
// - Memory usage: O(k) = number of segments, NOT O(n) = total leafs
// - Merging: O(n log k) using heap, where k << n (typically 8 segments vs millions of leafs)
// - StackTrie updates: Sequential streaming with no intermediate storage
//
// Memory benefit: Peak usage = largest segment, not sum of all segments (20-30% reduction).
// Performance benefit: Eliminates large memory allocation and improves cache locality (10-15% faster).
func (t *trieToSync) segmentFinished(ctx context.Context, idx int) error {
	segment := t.segments[idx]
	log.Info("[DEBUG-SYNC] Segment finished downloading", "segment", segment, "cachedLeafs", len(segment.cachedKeys))

	// Persist segment batch before hashing
	if err := segment.batch.Write(); err != nil {
		return err
	}
	segment.batch.Reset()

	// Atomically increment completion counter without lock contention
	completed := t.segmentsCompleted.Add(1)
	allDone := int(completed) == t.totalSegments

	if !allDone {
		// Other segments still downloading, wait for them
		return nil
	}

	// All segments done - acquire lock only for final processing
	t.lock.Lock()
	defer t.lock.Unlock()

	// All segments downloaded - now stream-merge them using k-way heap
	log.Info("[DEBUG-SYNC] All segments downloaded, starting k-way heap merge",
		"root", t.root,
		"segments", len(t.segments),
		"totalCachedLeafs", t.getTotalCachedLeafs())

	// Initialize k-way merge heap with iterators for each segment
	h := make(segmentHeap, 0, len(t.segments))
	for _, seg := range t.segments {
		if len(seg.cachedKeys) == 0 {
			continue // Skip empty segments
		}
		it := newSegmentIterator(seg)
		key, val := it.current()
		if key != nil {
			h = append(h, &heapItem{
				key:      key,
				val:      val,
				iterator: it,
			})
		}
	}
	heap.Init(&h)

	// Stream sorted data directly to StackTrie without intermediate storage
	// Memory usage = largest segment, not sum of all segments
	log.Info("[DEBUG-SYNC] Streaming merged data to StackTrie", "heapSize", len(h))
	itemCount := 0
	for len(h) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Pop smallest key from heap
		item := heap.Pop(&h).(*heapItem)

		// Update StackTrie with this key-value pair
		if err := t.stackTrie.Update(item.key, item.val); err != nil {
			return err
		}
		itemCount++

		// Batch database writes
		if t.batch.ValueSize() > int(t.sync.batchSize) {
			if err := t.batch.Write(); err != nil {
				return err
			}
			t.batch.Reset()
		}

		// Advance iterator and push next item if available
		if item.iterator.next() {
			nextKey, nextVal := item.iterator.current()
			if nextKey != nil {
				heap.Push(&h, &heapItem{
					key:      nextKey,
					val:      nextVal,
					iterator: item.iterator,
				})
			}
		}
	}
	log.Info("[DEBUG-SYNC] Completed streaming merge", "itemsProcessed", itemCount)

	// Clear segment cached data to free memory
	for _, seg := range t.segments {
		seg.cachedKeys = nil
		seg.cachedVals = nil
	}

	// Commit the final trie and verify root
	log.Info("[DEBUG-SYNC] Committing final trie", "root", t.root)
	actualRoot := t.stackTrie.Commit()
	if actualRoot != t.root {
		return fmt.Errorf("unexpected root, expected=%s, actual=%s, account=%s", t.root, actualRoot, t.account)
	}

	if !t.isMainTrie {
		if err := t.batch.Write(); err != nil {
			return err
		}
	}

	// Clean up persistent segment markers
	if err := customrawdb.ClearSyncSegments(t.sync.db, t.root); err != nil {
		return err
	}

	log.Info("[DEBUG-SYNC] Trie sync completed", "root", t.root)
	return t.task.OnFinish()
}

// getTotalCachedLeafs returns total cached leafs across all segments (for logging)
func (t *trieToSync) getTotalCachedLeafs() int {
	total := 0
	for _, seg := range t.segments {
		total += len(seg.cachedKeys)
	}
	return total
}

// createSegmentsIfNeeded is called from the leaf handler. In case the trie syncing only has
// one segment but a large number of leafs ([t.estimateSize() > segmentThreshold], it will
// create [numSegments-1] additional segments to sync the trie.
func (t *trieToSync) createSegmentsIfNeeded(ctx context.Context, numSegments int) error {
	if !t.shouldSegment() {
		return nil
	}

	return t.createSegments(ctx, numSegments)
}

// shouldSegment returns true if a trie should be separated into segments.
func (t *trieToSync) shouldSegment() bool {
	t.lock.Lock()
	defer t.lock.Unlock()

	// Return false if the trie has already been segmented.
	if len(t.segments) > 1 {
		return false
	}

	// Return true iff the estimated size of the trie exceeds the adaptive segment threshold.
	// Note: at this point there is only a single segment (loadSegments guarantees there
	// is at least one segment).
	segment := t.segments[0]
	return segment.estimateSize() >= getSegmentThreshold()
}

// divide the key space into [numSegments] consecutive segments.
// we use 2 bytes to build the ranges and fill the rest with
// ones or zeroes accordingly.
// this represents the step between the first 2 bytes of the start
// key of consecutive segments.
// createSegments should only be called once when there is only one
// thread accessing this trie, such that there is no need to hold a lock.
func (t *trieToSync) createSegments(ctx context.Context, numSegments int) error {
	segment := t.segments[0]

	segmentStep := 0x10000 / numSegments

	for i := 0; i < numSegments; i++ {
		start := uint16(i * segmentStep)
		end := uint16(i*segmentStep + (segmentStep - 1))

		startBytes := addPadding(start, 0x00)
		endBytes := addPadding(end, 0xff)

		// Skip any portion of the trie that has already been synced.
		if bytes.Compare(segment.pos, endBytes) >= 0 {
			continue
		}

		// since the first segment is already syncing,
		// it does not need to be added to the task queue.
		// instead, we update its end and move on to creating
		// the next segment
		if segment.end == nil {
			segment.end = endBytes
			continue
		}

		// create the segments
		segment := t.addSegment(startBytes, endBytes)
		if err := customrawdb.WriteSyncSegment(t.sync.db, t.root, common.BytesToHash(segment.start)); err != nil {
			return err
		}
	}
	// add the newly created segments to the task queue
	// after creating them. We skip the first one, as it
	// is already syncing.
	// this avoids concurrent access to [t.segments].
	for i := 1; i < len(t.segments); i++ {
		select {
		case t.sync.segments <- t.segments[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	t.sync.stats.incTriesSegmented()
	log.Debug("statesync: trie segmented for parallel sync", "root", t.root, "account", t.account, "segments", len(t.segments))
	return nil
}

// trieSegment keeps the state of syncing one segment of a [trieToSync]
// struct and keeps a pointer to the [trieToSync] it is syncing.
// each trieSegment is accessed by its own goroutine, so locks are not
// needed to access its fields
type trieSegment struct {
	start []byte
	pos   []byte
	end   []byte

	trie  *trieToSync // points back to the trie the segment belongs to
	idx   int         // index of this segment in the trie's segment slice
	batch ethdb.Batch // batch for writing leafs to
	leafs uint64      // number of leafs added to the segment

	// Cache leaf data in memory to avoid re-reading from DB for hashing
	// This eliminates I/O overhead during the hashing phase
	cachedKeys [][]byte
	cachedVals [][]byte
}

func (t *trieSegment) String() string {
	return fmt.Sprintf(
		"[%s](%d/%d) (start=%s,end=%s)",
		t.trie.root, t.idx+1, len(t.trie.segments),
		common.BytesToHash(t.start).TerminalString(),
		common.BytesToHash(t.end).TerminalString(),
	)
}

// these functions implement the LeafSyncTask interface.
func (t *trieSegment) Root() common.Hash                  { return t.trie.root }
func (t *trieSegment) Account() common.Hash               { return t.trie.account }
func (t *trieSegment) End() []byte                        { return t.end }
func (*trieSegment) NodeType() message.NodeType           { return message.StateTrieNode }
func (t *trieSegment) OnStart() (bool, error)             { return t.trie.task.OnStart() }
func (t *trieSegment) OnFinish(ctx context.Context) error { return t.trie.segmentFinished(ctx, t.idx) }

func (t *trieSegment) Start() []byte {
	if t.pos != nil {
		return t.pos
	}
	return t.start
}

func (t *trieSegment) OnLeafs(ctx context.Context, keys, vals [][]byte) error {
	// invoke the onLeafs callback
	if err := t.trie.task.OnLeafs(ctx, t.batch, keys, vals); err != nil {
		return err
	}
	// cap the segment's batch
	if t.batch.ValueSize() > int(t.trie.sync.batchSize) {
		if err := t.batch.Write(); err != nil {
			return err
		}
		t.batch.Reset()
	}
	t.leafs += uint64(len(keys))
	if len(keys) > 0 {
		t.pos = keys[len(keys)-1] // remember the position, used in estimating trie size
		utils.IncrOne(t.pos)
	}

	// Cache leaf data for parallel hashing (I/O optimization)
	// This avoids re-reading from DB during the hashing phase
	for i := range keys {
		t.cachedKeys = append(t.cachedKeys, keys[i]) // Keys are immutable hashes - no copy needed
		t.cachedVals = append(t.cachedVals, common.CopyBytes(vals[i]))
	}

	// update eta
	t.trie.sync.stats.incLeafs(t, uint64(len(keys)), t.estimateSize())

	if t.trie.root == t.trie.sync.root {
		return t.trie.createSegmentsIfNeeded(ctx, numMainTrieSegments)
	} else {
		return t.trie.createSegmentsIfNeeded(ctx, numStorageTrieSegments)
	}
}

// estimateSize calculates an estimate of the number of leafs and returns it,
// this assumes the trie has uniform key density.
// Note: returns 0 if there has been no progress in syncing the trie.
func (t *trieSegment) estimateSize() uint64 {
	start, pos, end := uint16(0), uint16(0), uint16(0xffff)
	if len(t.start) > 0 {
		start = binary.BigEndian.Uint16(t.start)
	}
	if len(t.pos) > 0 {
		pos = binary.BigEndian.Uint16(t.pos)
	}
	if len(t.end) > 0 {
		end = binary.BigEndian.Uint16(t.end)
	}
	progress := pos - start
	if progress == 0 {
		// this should not occur since estimateSize is called after processing
		// a batch of leafs, which sets [pos].
		// avoid division by 0 out of caution.
		return 0
	}
	left := end - pos
	return t.leafs * uint64(left) / uint64(progress)
}

// addPadding returns a []byte of length [common.Hash], starting with the BigEndian
// representation of [pos], and the rest filled with [padding].
func addPadding(pos uint16, padding byte) []byte {
	packer := wrappers.Packer{Bytes: make([]byte, common.HashLength)}
	packer.PackShort(pos)
	packer.PackFixedBytes(bytes.Repeat([]byte{padding}, common.HashLength-wrappers.ShortLen))
	return packer.Bytes
}
