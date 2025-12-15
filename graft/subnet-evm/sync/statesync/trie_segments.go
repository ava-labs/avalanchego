// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/avalanchego/utils/wrappers"

	syncclient "github.com/ava-labs/avalanchego/graft/subnet-evm/sync/client"
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

	// These fields are used to hash the segments in
	// order, even though they may finish syncing out
	// of order or concurrently.
	lock              sync.Mutex
	segmentsDone      map[int]struct{}
	segmentToHashNext int

	// We use a stack trie to hash the leafs and have
	// a batch used for writing it to disk.
	batch     ethdb.Batch
	stackTrie *trie.StackTrie

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
		sync:         sync,
		root:         root,
		account:      account,
		batch:        batch,
		stackTrie:    trie.NewStackTrie(&trie.StackTrieOptions{Writer: writeFn}),
		isMainTrie:   (root == sync.root),
		task:         syncTask,
		segmentsDone: make(map[int]struct{}),
	}
	return trieToSync, trieToSync.loadSegments()
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
		_, segmentStart := customrawdb.UnpackSyncSegmentKey(it.Key())
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

// startSyncing adds the trieToSync's segments to the work queue
func (t *trieToSync) startSyncing() {
	for _, segment := range t.segments {
		t.sync.segments <- segment // this will queue the segment for syncing
	}
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

// segmentFinished is called when one the trie segment with index [idx] finishes syncing.
// creates intermediary hash nodes for the trie up to the last contiguous segment received from start.
func (t *trieToSync) segmentFinished(ctx context.Context, idx int) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	log.Debug("statesync: segment finished", "segment", t.segments[idx])
	t.segmentsDone[idx] = struct{}{}
	for {
		if _, ok := t.segmentsDone[t.segmentToHashNext]; !ok {
			// if not the next contiguous segment from the beginning of the trie
			// don't do anything.
			break
		}
		segment := t.segments[t.segmentToHashNext]

		// persist any items in the batch as they will be iterated below.
		if err := segment.batch.Write(); err != nil {
			return err
		}
		segment.batch.Reset() // reset the batch to free memory (even though it is no longer used)

		// iterate all the items from the start of the segment (end is checked in the loop)
		it := t.task.IterateLeafs(common.BytesToHash(segment.start))
		defer it.Release()

		for it.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			if len(segment.end) > 0 && bytes.Compare(it.Key(), segment.end) > 0 {
				// don't go past the end of the segment. (data belongs to the next segment)
				break
			}
			// update the stack trie and cap the batch it writes to.
			value := common.CopyBytes(it.Value())
			if err := t.stackTrie.Update(it.Key(), value); err != nil {
				return err
			}
			if t.batch.ValueSize() > t.sync.batchSize {
				if err := t.batch.Write(); err != nil {
					return err
				}
				t.batch.Reset()
			}
		}
		if err := it.Error(); err != nil {
			return err
		}
		t.segmentToHashNext++
	}
	if t.segmentToHashNext < len(t.segments) {
		// trie not complete
		return nil
	}

	// when the trie is finished, this hashes any remaining nodes in the stack
	// trie and creates the root
	actualRoot := t.stackTrie.Commit()
	if actualRoot != t.root {
		return fmt.Errorf("unexpected root, expected=%s, actual=%s, account=%s", t.root, actualRoot, t.account)
	}
	if !t.isMainTrie {
		// the batch containing the main trie's root will be committed on
		// sync completion.
		if err := t.batch.Write(); err != nil {
			return err
		}
	}

	// remove all segments for this root from persistent storage
	if err := customrawdb.ClearSyncSegments(t.sync.db, t.root); err != nil {
		return err
	}
	return t.task.OnFinish()
}

// createSegmentsIfNeeded is called from the leaf handler. In case the trie syncing only has
// one segment but a large number of leafs ([t.estimateSize() > segmentThreshold], it will
// create [numSegments-1] additional segments to sync the trie.
func (t *trieToSync) createSegmentsIfNeeded(numSegments int) error {
	if !t.shouldSegment() {
		return nil
	}

	return t.createSegments(numSegments)
}

// shouldSegment returns true if a trie should be separated into segments.
func (t *trieToSync) shouldSegment() bool {
	t.lock.Lock()
	defer t.lock.Unlock()

	// Return false if the trie has already been segmented.
	if len(t.segments) > 1 {
		return false
	}

	// Return true iff the estimated size of the trie exceeds [segmentThreshold].
	// Note: at this point there is only a single segment (loadSegments guarantees there
	// is at least one segment).
	segment := t.segments[0]
	return segment.estimateSize() >= uint64(segmentThreshold)
}

// divide the key space into [numSegments] consecutive segments.
// we use 2 bytes to build the ranges and fill the rest with
// ones or zeroes accordingly.
// this represents the step between the first 2 bytes of the start
// key of consecutive segments.
// createSegments should only be called once when there is only one
// thread accessing this trie, such that there is no need to hold a lock.
func (t *trieToSync) createSegments(numSegments int) error {
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
		t.sync.segments <- t.segments[i]
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
func (t *trieSegment) OnStart() (bool, error)             { return t.trie.task.OnStart() }
func (t *trieSegment) OnFinish(ctx context.Context) error { return t.trie.segmentFinished(ctx, t.idx) }

func (t *trieSegment) Start() []byte {
	if t.pos != nil {
		return t.pos
	}
	return t.start
}

func (t *trieSegment) OnLeafs(keys, vals [][]byte) error {
	// invoke the onLeafs callback
	if err := t.trie.task.OnLeafs(t.batch, keys, vals); err != nil {
		return err
	}
	// cap the segment's batch
	if t.batch.ValueSize() > t.trie.sync.batchSize {
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

	// update eta
	t.trie.sync.stats.incLeafs(t, uint64(len(keys)), t.estimateSize())

	if t.trie.root == t.trie.sync.root {
		return t.trie.createSegmentsIfNeeded(numMainTrieSegments)
	} else {
		return t.trie.createSegmentsIfNeeded(numStorageTrieSegments)
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
