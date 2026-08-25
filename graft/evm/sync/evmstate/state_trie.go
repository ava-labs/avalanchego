// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"

	"github.com/ava-labs/avalanchego/graft/evm/sync/leaf"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
)

const (
	// segmentThreshold is the estimated leaf count above which a trie splits.
	segmentThreshold       = 500_000
	numMainTrieSegments    = 8
	numStorageTrieSegments = 4
)

var errRootMismatch = errors.New("reconstructed root does not match target")

// stateTrie reconstructs one EVM trie from concurrently fetched segments. Leaves
// land in the snapshot in any order, then feed a StackTrie re-read in key order.
type stateTrie struct {
	db      ethdb.KeyValueStore
	root    common.Hash
	account common.Hash
	leaves  leafStore

	batch     ethdb.Batch
	stackTrie *trie.StackTrie

	tasks       chan<- leaf.Task
	numSegments int
	threshold   uint64

	// isMainTrie defers the node write to the caller, so the account root is not
	// persisted before its storage tries finish.
	isMainTrie bool

	onDone func(context.Context) error

	stats *trieSyncStats

	lock              sync.Mutex
	segments          []*stateSegment
	segmentsDone      map[int]struct{}
	segmentToHashNext int
}

type stateTrieConfig struct {
	numSegments int
	threshold   uint64
	tasks       chan<- leaf.Task
	onDone      func(context.Context) error
	isMainTrie  bool
	stats       *trieSyncStats
}

func newStateTrie(db ethdb.KeyValueStore, root, account common.Hash, leaves leafStore, cfg stateTrieConfig) (*stateTrie, error) {
	if cfg.threshold == 0 {
		cfg.threshold = segmentThreshold
	}
	if cfg.numSegments == 0 {
		cfg.numSegments = numMainTrieSegments
	}

	batch := db.NewBatch()
	t := &stateTrie{
		db:      db,
		root:    root,
		account: account,
		leaves:  leaves,
		batch:   batch,
		stackTrie: trie.NewStackTrie(&trie.StackTrieOptions{
			Writer: func(path []byte, hash common.Hash, blob []byte) {
				rawdb.WriteTrieNode(batch, account, path, hash, blob, rawdb.HashScheme)
			},
		}),
		tasks:        cfg.tasks,
		numSegments:  cfg.numSegments,
		threshold:    cfg.threshold,
		onDone:       cfg.onDone,
		isMainTrie:   cfg.isMainTrie,
		stats:        cfg.stats,
		segmentsDone: make(map[int]struct{}),
	}
	if err := t.loadSegments(); err != nil {
		return nil, err
	}
	return t, nil
}

// loadSegments restores segments from a prior run and advances each past the leaves
// already in the snapshot, so a restart skips them. A fresh trie loads one segment.
func (t *stateTrie) loadSegments() error {
	it := customrawdb.NewSyncSegmentsIterator(t.db, t.root)
	defer it.Release()

	// Each marker is a segment start. The prior segment ends just below it. The
	// first segment's start is never persisted, so prevStart reconstructs it.
	var prevStart []byte
	for it.Next() {
		_, segmentStart := customrawdb.ParseSyncSegmentKey(it.Key())
		startPos := binary.BigEndian.Uint16(segmentStart[:wrappers.ShortLen])
		t.addSegment(prevStart, addPadding(startPos-1, 0xff))
		prevStart = segmentStart
	}
	if err := it.Error(); err != nil {
		return err
	}
	t.addSegment(prevStart, nil)

	for _, segment := range t.segments {
		if err := t.loadSegmentPos(segment); err != nil {
			return err
		}
	}
	return nil
}

// loadSegmentPos advances a segment's position past the leaves already synced.
func (t *stateTrie) loadSegmentPos(segment *stateSegment) error {
	it := t.leaves.iterateLeaves(common.BytesToHash(segment.start))
	defer it.Release()

	var lastKey []byte
	for it.Next() {
		if !leaf.WithinRange(it.Key(), segment.end) {
			break
		}
		lastKey = common.CopyBytes(it.Key())
		segment.leafCount++
	}
	if err := it.Error(); err != nil {
		return err
	}
	if lastKey != nil {
		segment.pos = leaf.NextRangeKey(lastKey)
	}
	return nil
}

func (t *stateTrie) addSegment(start, end []byte) *stateSegment {
	segment := &stateSegment{
		trie:  t,
		start: start,
		end:   end,
		idx:   len(t.segments),
		batch: t.db.NewBatch(),
	}
	t.segments = append(t.segments, segment)
	return segment
}

// flushProgress writes each segment's buffered leaves, so an interrupted sync resumes
// from them. Only safe with no worker running on this trie.
func (t *stateTrie) flushProgress() error {
	for _, segment := range t.segments {
		if err := segment.batch.Write(); err != nil {
			return err
		}
	}
	return nil
}

// commitNodes writes the StackTrie's buffered nodes, including the root.
func (t *stateTrie) commitNodes() error {
	return t.batch.Write()
}

// segmentFinished hashes every contiguous finished segment in key order, then
// verifies and commits the root once all segments are done.
func (t *stateTrie) segmentFinished(ctx context.Context, idx int) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.segmentsDone[idx] = struct{}{}
	for {
		if _, ok := t.segmentsDone[t.segmentToHashNext]; !ok {
			// Not the next contiguous segment yet.
			break
		}
		if err := t.hashSegment(ctx, t.segments[t.segmentToHashNext]); err != nil {
			return err
		}
		t.segmentToHashNext++
	}
	if t.segmentToHashNext < len(t.segments) {
		return nil
	}

	if root := t.stackTrie.Commit(); root != t.root {
		return fmt.Errorf("%w: got %x want %x", errRootMismatch, root, t.root)
	}
	if !t.isMainTrie {
		if err := t.commitNodes(); err != nil {
			return err
		}
	}
	if err := customrawdb.ClearSyncSegments(t.db, t.root); err != nil {
		return err
	}
	if t.onDone != nil {
		return t.onDone(ctx)
	}
	return nil
}

// hashSegment flushes the segment's leaves, then re-reads them in key order.
func (t *stateTrie) hashSegment(ctx context.Context, segment *stateSegment) error {
	if err := segment.batch.Write(); err != nil {
		return err
	}
	segment.batch.Reset()

	it := t.leaves.iterateLeaves(common.BytesToHash(segment.start))
	defer it.Release()
	for it.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !leaf.WithinRange(it.Key(), segment.end) {
			// Belongs to the next segment.
			break
		}
		if err := t.stackTrie.Update(it.Key(), common.CopyBytes(it.Value())); err != nil {
			return err
		}
		if err := flushIfFull(t.batch); err != nil {
			return err
		}
	}
	return it.Error()
}

// createSegmentsIfNeeded splits the trie past the threshold, so the rest of the key
// space fetches concurrently.
func (t *stateTrie) createSegmentsIfNeeded(ctx context.Context, segment *stateSegment) error {
	t.lock.Lock()
	if len(t.segments) > 1 || segment.estimateSize() < t.threshold {
		t.lock.Unlock()
		return nil
	}
	t.lock.Unlock()
	return t.createSegments(ctx)
}

// createSegments splits the prefix space, extends the first segment, and queues the
// rest. One goroutine owns the trie while it runs, so it needs no lock.
func (t *stateTrie) createSegments(ctx context.Context) error {
	first := t.segments[0]

	for i := range t.numSegments {
		startBytes, endBytes := segmentRange(i, t.numSegments)

		// Skip ranges the first segment already covered.
		if bytes.Compare(first.pos, endBytes) >= 0 {
			continue
		}
		if first.end == nil {
			first.end = endBytes
			continue
		}
		segment := t.addSegment(startBytes, endBytes)
		if err := customrawdb.WriteSyncSegment(t.db, t.root, common.BytesToHash(segment.start)); err != nil {
			return err
		}
	}

	// Queue the new segments. The first is already syncing.
	for i := 1; i < len(t.segments); i++ {
		select {
		case t.tasks <- t.segments[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// stateSegment is one contiguous key range of a [stateTrie].
type stateSegment struct {
	trie      *stateTrie
	start     []byte
	pos       []byte
	end       []byte
	idx       int
	batch     ethdb.Batch
	leafCount uint64
}

func (s *stateSegment) Root() common.Hash    { return s.trie.root }
func (s *stateSegment) Account() common.Hash { return s.trie.account }
func (s *stateSegment) End() []byte          { return s.end }

func (s *stateSegment) Start() []byte {
	if s.pos != nil {
		return s.pos
	}
	return s.start
}

func (s *stateSegment) OnLeaves(ctx context.Context, leaves evmstate.Leaves) error {
	if err := s.trie.leaves.writeLeaves(ctx, s.batch, leaves); err != nil {
		return err
	}
	if err := flushIfFull(s.batch); err != nil {
		return err
	}
	s.leafCount += uint64(len(leaves.Keys))
	if len(leaves.Keys) > 0 {
		s.pos = leaf.NextRangeKey(leaves.Keys[len(leaves.Keys)-1])
	}
	if s.trie.stats != nil {
		s.trie.stats.incLeaves(s, uint64(len(leaves.Keys)), s.estimateSize())
	}
	return s.trie.createSegmentsIfNeeded(ctx, s)
}

func (s *stateSegment) OnFinish(ctx context.Context) error {
	return s.trie.segmentFinished(ctx, s.idx)
}

// estimateSize approximates the leaf count from the prefix density covered so far,
// assuming uniform keys. Returns 0 before any progress.
func (s *stateSegment) estimateSize() uint64 {
	start, pos, end := uint16(0), uint16(0), uint16(0xffff)
	if len(s.start) > 0 {
		start = binary.BigEndian.Uint16(s.start)
	}
	if len(s.pos) > 0 {
		pos = binary.BigEndian.Uint16(s.pos)
	}
	if len(s.end) > 0 {
		end = binary.BigEndian.Uint16(s.end)
	}
	progress := pos - start
	if progress == 0 {
		return 0
	}
	left := end - pos
	return s.leafCount * uint64(left) / uint64(progress)
}

// flushIfFull writes and resets batch past [ethdb.IdealBatchSize], capping memory.
func flushIfFull(batch ethdb.Batch) error {
	if batch.ValueSize() <= ethdb.IdealBatchSize {
		return nil
	}
	if err := batch.Write(); err != nil {
		return err
	}
	batch.Reset()
	return nil
}
