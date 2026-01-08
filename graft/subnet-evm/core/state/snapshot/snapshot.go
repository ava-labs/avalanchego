// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
// Copyright 2019 The go-ethereum Authors
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

// Package snapshot implements a journalled, dynamic state dump.
package snapshot

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	ethsnapshot "github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/stateconf"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/metrics"
	"github.com/ava-labs/libevm/triedb"
)

const (
	// skipGenThreshold is the minimum time that must have elapsed since the
	// creation of the previous disk layer to start snapshot generation on a new
	// disk layer.
	//
	// If disk layers are being discarded at a frequency greater than this threshold,
	// starting snapshot generation is not worth it (will be aborted before meaningful
	// work can be done).
	skipGenThreshold = 500 * time.Millisecond
)

// ====== If resolving merge conflicts ======
//
// All calls to metrics.NewRegistered*() for metrics also defined in libevm/core/state/snapshot
// have been replaced with metrics.GetOrRegister*() to get metrics already registered in
// libevm/core/state/snapshot or register them here otherwise. These replacements ensure the
// same metrics are shared between the two packages.
var (
	snapshotCleanAccountHitMeter   = metrics.GetOrRegisterMeter("state/snapshot/clean/account/hit", nil)
	snapshotCleanAccountMissMeter  = metrics.GetOrRegisterMeter("state/snapshot/clean/account/miss", nil)
	snapshotCleanAccountInexMeter  = metrics.GetOrRegisterMeter("state/snapshot/clean/account/inex", nil)
	snapshotCleanAccountReadMeter  = metrics.GetOrRegisterMeter("state/snapshot/clean/account/read", nil)
	snapshotCleanAccountWriteMeter = metrics.GetOrRegisterMeter("state/snapshot/clean/account/write", nil)

	snapshotCleanStorageHitMeter   = metrics.GetOrRegisterMeter("state/snapshot/clean/storage/hit", nil)
	snapshotCleanStorageMissMeter  = metrics.GetOrRegisterMeter("state/snapshot/clean/storage/miss", nil)
	snapshotCleanStorageInexMeter  = metrics.GetOrRegisterMeter("state/snapshot/clean/storage/inex", nil)
	snapshotCleanStorageReadMeter  = metrics.GetOrRegisterMeter("state/snapshot/clean/storage/read", nil)
	snapshotCleanStorageWriteMeter = metrics.GetOrRegisterMeter("state/snapshot/clean/storage/write", nil)

	snapshotDirtyAccountHitMeter   = metrics.GetOrRegisterMeter("state/snapshot/dirty/account/hit", nil)
	snapshotDirtyAccountMissMeter  = metrics.GetOrRegisterMeter("state/snapshot/dirty/account/miss", nil)
	snapshotDirtyAccountInexMeter  = metrics.GetOrRegisterMeter("state/snapshot/dirty/account/inex", nil)
	snapshotDirtyAccountReadMeter  = metrics.GetOrRegisterMeter("state/snapshot/dirty/account/read", nil)
	snapshotDirtyAccountWriteMeter = metrics.GetOrRegisterMeter("state/snapshot/dirty/account/write", nil)

	snapshotDirtyStorageHitMeter   = metrics.GetOrRegisterMeter("state/snapshot/dirty/storage/hit", nil)
	snapshotDirtyStorageMissMeter  = metrics.GetOrRegisterMeter("state/snapshot/dirty/storage/miss", nil)
	snapshotDirtyStorageInexMeter  = metrics.GetOrRegisterMeter("state/snapshot/dirty/storage/inex", nil)
	snapshotDirtyStorageReadMeter  = metrics.GetOrRegisterMeter("state/snapshot/dirty/storage/read", nil)
	snapshotDirtyStorageWriteMeter = metrics.GetOrRegisterMeter("state/snapshot/dirty/storage/write", nil)

	snapshotDirtyAccountHitDepthHist = metrics.GetOrRegisterHistogram("state/snapshot/dirty/account/hit/depth", nil, metrics.NewExpDecaySample(1028, 0.015))
	snapshotDirtyStorageHitDepthHist = metrics.GetOrRegisterHistogram("state/snapshot/dirty/storage/hit/depth", nil, metrics.NewExpDecaySample(1028, 0.015))

	snapshotFlushAccountItemMeter = metrics.GetOrRegisterMeter("state/snapshot/flush/account/item", nil)
	snapshotFlushAccountSizeMeter = metrics.GetOrRegisterMeter("state/snapshot/flush/account/size", nil)
	snapshotFlushStorageItemMeter = metrics.GetOrRegisterMeter("state/snapshot/flush/storage/item", nil)
	snapshotFlushStorageSizeMeter = metrics.GetOrRegisterMeter("state/snapshot/flush/storage/size", nil)

	snapshotBloomIndexTimer = metrics.GetOrRegisterResettingTimer("state/snapshot/bloom/index", nil)
	snapshotBloomErrorGauge = metrics.GetOrRegisterGaugeFloat64("state/snapshot/bloom/error", nil)

	snapshotBloomAccountTrueHitMeter  = metrics.GetOrRegisterMeter("state/snapshot/bloom/account/truehit", nil)
	snapshotBloomAccountFalseHitMeter = metrics.GetOrRegisterMeter("state/snapshot/bloom/account/falsehit", nil)
	snapshotBloomAccountMissMeter     = metrics.GetOrRegisterMeter("state/snapshot/bloom/account/miss", nil)

	snapshotBloomStorageTrueHitMeter  = metrics.GetOrRegisterMeter("state/snapshot/bloom/storage/truehit", nil)
	snapshotBloomStorageFalseHitMeter = metrics.GetOrRegisterMeter("state/snapshot/bloom/storage/falsehit", nil)
	snapshotBloomStorageMissMeter     = metrics.GetOrRegisterMeter("state/snapshot/bloom/storage/miss", nil)

	// ErrSnapshotStale is returned from data accessors if the underlying snapshot
	// layer had been invalidated due to the chain progressing forward far enough
	// to not maintain the layer's original state.
	ErrSnapshotStale = errors.New("snapshot stale")

	// ErrStaleParentLayer is returned when Flatten attempts to flatten a diff layer into
	// a stale parent.
	ErrStaleParentLayer = errors.New("parent disk layer is stale")

	// ErrNotCoveredYet is returned from data accessors if the underlying snapshot
	// is being generated currently and the requested data item is not yet in the
	// range of accounts covered.
	ErrNotCoveredYet = errors.New("not covered yet")

	// ErrNotConstructed is returned if the callers want to iterate the snapshot
	// while the generation is not finished yet.
	ErrNotConstructed = errors.New("snapshot is not constructed")
)

// Snapshot represents the functionality supported by a snapshot storage layer.
type Snapshot = ethsnapshot.Snapshot

// snapshot is the internal version of the snapshot data layer that supports some
// additional methods compared to the public API.
type snapshot interface {
	Snapshot

	BlockHash() common.Hash

	// Parent returns the subsequent layer of a snapshot, or nil if the base was
	// reached.
	//
	// Note, the method is an internal helper to avoid type switching between the
	// disk and diff layers. There is no locking involved.
	Parent() snapshot

	// Update creates a new layer on top of the existing snapshot diff tree with
	// the specified data items.
	//
	// Note, the maps are retained by the method to avoid copying everything.
	Update(blockHash, blockRoot common.Hash, destructs map[common.Hash]struct{}, accounts map[common.Hash][]byte, storage map[common.Hash]map[common.Hash][]byte) *diffLayer

	// Stale return whether this layer has become stale (was flattened across) or
	// if it's still live.
	Stale() bool

	// AccountIterator creates an account iterator over an arbitrary layer.
	AccountIterator(seek common.Hash) AccountIterator

	// StorageIterator creates a storage iterator over an arbitrary layer.
	StorageIterator(account common.Hash, seek common.Hash) (StorageIterator, bool)
}

// Config includes the configurations for snapshots.
type Config struct {
	CacheSize  int  // Megabytes permitted to use for read caches
	NoBuild    bool // Indicator that the snapshots generation is disallowed
	AsyncBuild bool // The snapshot generation is allowed to be constructed asynchronously
	SkipVerify bool // Indicator that all verification should be bypassed
}

// Tree is an Ethereum state snapshot tree. It consists of one persistent base
// layer backed by a key-value store, on top of which arbitrarily many in-memory
// diff layers are topped. The memory diffs can form a tree with branching, but
// the disk layer is singleton and common to all. If a reorg goes deeper than the
// disk layer, everything needs to be deleted.
//
// The goal of a state snapshot is twofold: to allow direct access to account and
// storage data to avoid expensive multi-level trie lookups; and to allow sorted,
// cheap iteration of the account/storage tries for sync aid.
type Tree struct {
	config Config              // Snapshots configurations
	diskdb ethdb.KeyValueStore // Persistent database to store the snapshot
	triedb *triedb.Database    // In-memory cache to access the trie through
	// Collection of all known layers
	// blockHash -> snapshot
	blockLayers map[common.Hash]snapshot
	// stateRoot -> blockHash -> snapshot
	// Update creates a new block layer with a parent taken from the blockHash -> snapshot map
	// we can support grabbing a read only Snapshot by getting any one from the state root based map
	stateLayers map[common.Hash]map[common.Hash]snapshot
	verified    bool // Indicates if snapshot integrity has been verified
	lock        sync.RWMutex

	// Test hooks
	onFlatten func() // Hook invoked when the bottom most diff layers are flattened
}

// New attempts to load an already existing snapshot from a persistent key-value
// store (with a number of memory layers from a journal), ensuring that the head
// of the snapshot matches the expected one.
//
// If the snapshot is missing or the disk layer is broken, the snapshot will be
// reconstructed using both the existing data and the state trie.
// The repair happens on a background thread.
func New(config Config, diskdb ethdb.KeyValueStore, triedb *triedb.Database, blockHash, root common.Hash) (*Tree, error) {
	// Create a new, empty snapshot tree
	snap := &Tree{
		config:      config,
		diskdb:      diskdb,
		triedb:      triedb,
		blockLayers: make(map[common.Hash]snapshot),
		stateLayers: make(map[common.Hash]map[common.Hash]snapshot),
		verified:    config.SkipVerify, // if SkipVerify is true, all verification will be bypassed
	}

	// Attempt to load a previously persisted snapshot and rebuild one if failed
	head, generated, err := loadSnapshot(diskdb, triedb, config.CacheSize, blockHash, root, config.NoBuild)
	if err != nil {
		log.Warn("Failed to load snapshot, regenerating", "err", err)
		if !config.NoBuild {
			snap.Rebuild(blockHash, root)
			if !config.AsyncBuild {
				if err := snap.verifyIntegrity(snap.disklayer(), true); err != nil {
					return nil, err
				}
			}
			return snap, nil
		}
		return nil, err // Bail out the error, don't rebuild automatically.
	}

	// Existing snapshot loaded, seed all the layers
	// It is unnecessary to grab the lock here, since it was created within this function
	// call, but we grab it nevertheless to follow the spec for insertSnap.
	snap.lock.Lock()
	defer snap.lock.Unlock()
	for head != nil {
		snap.insertSnap(head)
		head = head.Parent()
	}

	// Verify any synchronously generated or loaded snapshot from disk
	if !config.AsyncBuild || generated {
		if err := snap.verifyIntegrity(snap.disklayer(), !config.AsyncBuild && !generated); err != nil {
			return nil, err
		}
	}

	return snap, nil
}

// insertSnap inserts [snap] into the tree.
// Assumes the lock is held.
func (t *Tree) insertSnap(snap snapshot) {
	t.blockLayers[snap.BlockHash()] = snap
	blockSnaps, ok := t.stateLayers[snap.Root()]
	if !ok {
		blockSnaps = make(map[common.Hash]snapshot)
		t.stateLayers[snap.Root()] = blockSnaps
	}
	blockSnaps[snap.BlockHash()] = snap
}

// Snapshot retrieves a snapshot belonging to the given state root, or nil if no
// snapshot is maintained for that state root.
func (t *Tree) Snapshot(stateRoot common.Hash) Snapshot {
	return t.getSnapshot(stateRoot, false)
}

// getSnapshot retrieves a Snapshot by its state root. If the caller already holds the
// snapTree lock when callthing this function, [holdsTreeLock] should be set to true.
func (t *Tree) getSnapshot(stateRoot common.Hash, holdsTreeLock bool) snapshot {
	if !holdsTreeLock {
		t.lock.RLock()
		defer t.lock.RUnlock()
	}

	layers := t.stateLayers[stateRoot]
	for _, layer := range layers {
		return layer
	}
	return nil
}

// Snapshots returns all visited layers from the topmost layer with specific
// root and traverses downward. The layer amount is limited by the given number.
// If nodisk is set, then disk layer is excluded.
func (t *Tree) Snapshots(blockHash common.Hash, limits int, nodisk bool) []Snapshot {
	t.lock.RLock()
	defer t.lock.RUnlock()

	if limits == 0 {
		return nil
	}
	layer, ok := t.blockLayers[blockHash]
	if !ok {
		return nil
	}
	var ret []Snapshot
	for {
		if _, isdisk := layer.(*diskLayer); isdisk && nodisk {
			break
		}
		ret = append(ret, layer)
		limits -= 1
		if limits == 0 {
			break
		}
		parent := layer.Parent()
		if parent == nil {
			break
		}
		layer = parent
	}
	return ret
}

type blockHashes struct {
	blockHash       common.Hash
	parentBlockHash common.Hash
}

func WithBlockHashes(blockHash, parentBlockHash common.Hash) stateconf.SnapshotUpdateOption {
	return stateconf.WithSnapshotUpdatePayload(blockHashes{blockHash, parentBlockHash})
}

// Update adds a new snapshot into the tree, if that can be linked to an existing
// old parent. It is disallowed to insert a disk layer (the origin of all).
func (t *Tree) Update(
	blockRoot common.Hash,
	parentRoot common.Hash,
	destructs map[common.Hash]struct{},
	accounts map[common.Hash][]byte,
	storage map[common.Hash]map[common.Hash][]byte,
	opts ...stateconf.SnapshotUpdateOption,
) error {
	if len(opts) == 0 {
		return fmt.Errorf("missing block hashes")
	}

	payload := stateconf.ExtractSnapshotUpdatePayload(opts[0])
	p, ok := payload.(blockHashes)
	if !ok {
		return fmt.Errorf("invalid block hashes payload type: %T", payload)
	}

	return t.UpdateWithBlockHashes(p.blockHash, blockRoot, p.parentBlockHash, destructs, accounts, storage)
}

func (t *Tree) UpdateWithBlockHashes(
	blockHash, blockRoot, parentBlockHash common.Hash,
	destructs map[common.Hash]struct{},
	accounts map[common.Hash][]byte,
	storage map[common.Hash]map[common.Hash][]byte,
) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	// Grab the parent snapshot based on the parent block hash, not the parent state root
	parent := t.blockLayers[parentBlockHash]
	if parent == nil {
		return fmt.Errorf("parent [%#x] snapshot missing", parentBlockHash)
	}

	snap := t.blockLayers[blockHash]
	if snap != nil {
		log.Warn("Attempted to insert a snapshot layer for an existing block",
			"blockHash", blockHash, "blockRoot", blockRoot, "parentHash", parentBlockHash,
			"existingBlockRoot", snap.Root(),
		)
	}

	snap = parent.Update(blockHash, blockRoot, destructs, accounts, storage)
	t.insertSnap(snap)
	return nil
}

// verifyIntegrity performs an integrity check on the current snapshot using
// verify. Most importantly, verifyIntegrity ensures verify is called at
// most once during the entire lifetime of [Tree], returning immediately if
// already invoked. If [waitBuild] is true, verifyIntegrity will wait for
// generation of the snapshot to finish before verifying.
//
// It is assumed that the caller holds the [snapTree] lock
// when calling this function.
func (t *Tree) verifyIntegrity(base *diskLayer, waitBuild bool) error {
	// Find the rebuild termination channel and wait until
	// the snapshot is generated
	if done := base.genPending; waitBuild && done != nil {
		log.Info("Waiting for snapshot generation", "root", base.root)
		<-done
	}

	if t.verified {
		return nil
	}

	if base.genMarker != nil {
		return errors.New("cannot verify integrity of an unfinished snapshot")
	}

	start := time.Now()
	log.Info("Verifying snapshot integrity", "root", base.root)
	if err := t.verify(base.root, true); err != nil {
		return fmt.Errorf("unable to verify snapshot integrity: %w", err)
	}

	log.Info("Verified snapshot integrity", "root", base.root, "elapsed", time.Since(start))
	t.verified = true
	return nil
}

func (t *Tree) Cap(root common.Hash, layers int) error {
	return nil // No-op as this code uses Flatten on block accept instead
}

// Flatten flattens the snapshot for [blockHash] into its parent. if its
// parent is not a disk layer, Flatten will return an error.
// Note: a blockHash is used instead of a state root so that the exact state
// transition between the two states is well defined. This is intended to
// prevent the following edge case
//
//	  A
//	 /  \
//	B    C
//	     |
//	     D
//
// In this scenario, it's possible For (A, B) and (A, C, D) to be two
// different paths to the resulting state. We use block hashes and parent
// block hashes to ensure that the exact path through which we flatten
// diffLayers is well defined.
func (t *Tree) Flatten(blockHash common.Hash) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	start := time.Now()
	snap, ok := t.blockLayers[blockHash]
	if !ok {
		return fmt.Errorf("cannot flatten missing snapshot: %s", blockHash)
	}
	diff, ok := snap.(*diffLayer)
	if !ok {
		return fmt.Errorf("cannot flatten disk layer: (%s, %s)", blockHash, snap.Root())
	}
	if diff.parent == nil {
		return fmt.Errorf("cannot flatten snapshot with missing parent (%s, %s)", blockHash, diff.root)
	}
	if parentDiff, ok := diff.parent.(*diffLayer); ok {
		return fmt.Errorf("cannot flatten snapshot (%s, %s) into diff layer parent (%s, %s)", blockHash, diff.root, parentDiff.blockHash, parentDiff.root)
	}
	parentLayer := t.blockLayers[diff.parent.BlockHash()]
	if parentLayer == nil {
		return fmt.Errorf("snapshot missing parent layer: %s", diff.parent.BlockHash())
	}

	diff.lock.Lock()
	// Invoke the hook if it's registered. Ugly hack.
	if t.onFlatten != nil {
		t.onFlatten()
	}
	base, snapshotGenerated, err := diffToDisk(diff)
	diff.lock.Unlock()
	if err != nil {
		return err
	}

	// Remove parent layer
	if err := t.discard(diff.parent.BlockHash(), true); err != nil {
		return fmt.Errorf("failed to discard parent layer while flattening (%s, %s): %w", blockHash, diff.root, err)
	}
	// We created a new diskLayer [base] to replace [diff], so we need to replace
	// it in both maps and replace all pointers to it.
	t.blockLayers[base.blockHash] = base
	stateSnaps := t.stateLayers[base.root]
	// stateSnaps must already be initialized here, since we are replacing
	// an existing snapshot instead of adding a new one.
	stateSnaps[base.blockHash] = base

	// Replace the parent pointers for any snapshot that referenced
	// the replaced diffLayer.
	for _, snap := range t.blockLayers {
		if diff, ok := snap.(*diffLayer); ok {
			if base.blockHash == diff.parent.BlockHash() {
				diff.lock.Lock()
				diff.parent = base
				diff.lock.Unlock()
			}
		}
	}

	// TODO add tracking of children to the snapshots to reduce overhead here.
	children := make(map[common.Hash][]common.Hash)
	for blockHash, snap := range t.blockLayers {
		if diff, ok := snap.(*diffLayer); ok {
			parent := diff.parent.BlockHash()
			children[parent] = append(children[parent], blockHash)
		}
	}
	var remove func(blockHash common.Hash)
	remove = func(blockHash common.Hash) {
		t.discard(blockHash, false)
		for _, child := range children[blockHash] {
			remove(child)
		}
		delete(children, blockHash)
	}
	for blockHash, snap := range t.blockLayers {
		if snap.Stale() {
			remove(blockHash)
		}
	}
	// If the disk layer was modified, regenerate all the cumulative blooms
	var rebloom func(blockHash common.Hash)
	rebloom = func(blockHash common.Hash) {
		if diff, ok := t.blockLayers[blockHash].(*diffLayer); ok {
			diff.rebloom(base)
		}
		for _, child := range children[blockHash] {
			rebloom(child)
		}
	}
	rebloom(base.blockHash)
	log.Debug("Flattened snapshot tree", "blockHash", blockHash, "root", base.root, "size", len(t.blockLayers), "elapsed", common.PrettyDuration(time.Since(start)))

	if !snapshotGenerated {
		return nil
	}
	return t.verifyIntegrity(base, false)
}

// Length returns the number of snapshot layers that is currently being maintained.
func (t *Tree) NumStateLayers() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return len(t.stateLayers)
}

func (t *Tree) NumBlockLayers() int {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return len(t.blockLayers)
}

// Discard removes layers that we no longer need
func (t *Tree) Discard(blockHash common.Hash) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.discard(blockHash, false)
}

// discard removes the snapshot associated with [blockHash] from the
// snapshot tree.
// If [force] is true, discard may delete the disk layer. This should
// only be called within Flatten, when a new disk layer is being created.
// Assumes the lock is held.
func (t *Tree) discard(blockHash common.Hash, force bool) error {
	snap := t.blockLayers[blockHash]
	if snap == nil {
		return fmt.Errorf("cannot discard missing snapshot: %s", blockHash)
	}
	_, ok := snap.(*diffLayer)
	// Never discard the disk layer
	if !ok && !force {
		return fmt.Errorf("cannot discard the disk layer: %s", blockHash)
	}
	snaps, ok := t.stateLayers[snap.Root()]
	if !ok {
		return fmt.Errorf("cannot discard snapshot %s missing from state: %s", blockHash, snap.Root())
	}
	// Discard the block from the map. If there are no more blocks
	// mapping to the same state remove it from [stateLayers] as well.
	delete(snaps, blockHash)
	if len(snaps) == 0 {
		delete(t.stateLayers, snap.Root())
	}
	delete(t.blockLayers, blockHash)
	return nil
}

// AbortGeneration aborts an ongoing snapshot generation process (if it hasn't
// stopped already).
//
// It is not required to manually abort snapshot generation. If generation has not
// been manually aborted prior to invoking [diffToDisk], it will be aborted anyways.
//
// It is safe to call this method multiple times and when there is no snapshot
// generation currently underway.
func (t *Tree) AbortGeneration() {
	t.lock.Lock()
	defer t.lock.Unlock()

	dl := t.disklayer()
	dl.abortGeneration()
}

// abortGeneration sends an abort message to the generate goroutine and waits
// for it to shutdown before returning (if it is running). This call should not
// be made concurrently.
func (dl *diskLayer) abortGeneration() bool {
	// Store ideal time for abort to get better estimate of load
	//
	// Note that we set this time regardless if abortion was skipped otherwise we
	// will never restart generation (age will always be negative).
	if dl.abortStarted.IsZero() {
		dl.abortStarted = time.Now()
	}

	// If the disk layer is running a snapshot generator, abort it
	dl.lock.RLock()
	shouldAbort := dl.genAbort != nil && dl.genStats == nil
	dl.lock.RUnlock()
	if shouldAbort {
		abort := make(chan struct{})
		dl.genAbort <- abort
		<-abort
		return true
	}

	return false
}

// diffToDisk merges a bottom-most diff into the persistent disk layer underneath
// it. The method will panic if called onto a non-bottom-most diff layer.
//
// The disk layer persistence should be operated in an atomic way. All updates should
// be discarded if the whole transition if not finished.
func diffToDisk(bottom *diffLayer) (*diskLayer, bool, error) {
	var (
		base  = bottom.parent.(*diskLayer)
		batch = base.diskdb.NewBatch()
	)

	// Attempt to abort generation (if not already aborted)
	base.abortGeneration()

	// Put the deletion in the batch writer, flush all updates in the final step.
	customrawdb.DeleteSnapshotBlockHash(batch)
	rawdb.DeleteSnapshotRoot(batch)

	// Mark the original base as stale as we're going to create a new wrapper
	base.lock.Lock()
	if base.stale {
		base.lock.Unlock()
		return nil, false, ErrStaleParentLayer // we've committed into the same base from two children, boo
	}
	base.stale = true
	base.lock.Unlock()

	// Destroy all the destructed accounts from the database
	for hash := range bottom.destructSet {
		// Skip any account not covered yet by the snapshot
		if base.genMarker != nil && bytes.Compare(hash[:], base.genMarker) > 0 {
			continue
		}
		// Remove all storage slots
		rawdb.DeleteAccountSnapshot(batch, hash)
		base.cache.Set(hash[:], nil)

		it := rawdb.IterateStorageSnapshots(base.diskdb, hash)
		for it.Next() {
			key := it.Key()
			batch.Delete(key)
			base.cache.Del(key[1:])
			snapshotFlushStorageItemMeter.Mark(1)

			// Ensure we don't delete too much data blindly (contract can be
			// huge). It's ok to flush, the root will go missing in case of a
			// crash and we'll detect and regenerate the snapshot.
			if batch.ValueSize() > 64*1024*1024 {
				if err := batch.Write(); err != nil {
					log.Crit("Failed to write storage deletions", "err", err)
				}
				batch.Reset()
			}
		}
		it.Release()
	}
	// Push all updated accounts into the database
	for hash, data := range bottom.accountData {
		// Skip any account not covered yet by the snapshot
		if base.genMarker != nil && bytes.Compare(hash[:], base.genMarker) > 0 {
			continue
		}
		// Push the account to disk
		rawdb.WriteAccountSnapshot(batch, hash, data)
		base.cache.Set(hash[:], data)
		snapshotCleanAccountWriteMeter.Mark(int64(len(data)))

		snapshotFlushAccountItemMeter.Mark(1)
		snapshotFlushAccountSizeMeter.Mark(int64(len(data)))

		// Ensure we don't write too much data blindly. It's ok to flush, the
		// root will go missing in case of a crash and we'll detect and regen
		// the snapshot.
		if batch.ValueSize() > 64*1024*1024 {
			if err := batch.Write(); err != nil {
				log.Crit("Failed to write storage deletions", "err", err)
			}
			batch.Reset()
		}
	}
	// Push all the storage slots into the database
	for accountHash, storage := range bottom.storageData {
		// Skip any account not covered yet by the snapshot
		if base.genMarker != nil && bytes.Compare(accountHash[:], base.genMarker) > 0 {
			continue
		}
		// Generation might be mid-account, track that case too
		midAccount := base.genMarker != nil && bytes.Equal(accountHash[:], base.genMarker[:common.HashLength])

		for storageHash, data := range storage {
			// Skip any slot not covered yet by the snapshot
			if midAccount && bytes.Compare(storageHash[:], base.genMarker[common.HashLength:]) > 0 {
				continue
			}
			if len(data) > 0 {
				rawdb.WriteStorageSnapshot(batch, accountHash, storageHash, data)
				base.cache.Set(append(accountHash[:], storageHash[:]...), data)
				snapshotCleanStorageWriteMeter.Mark(int64(len(data)))
			} else {
				rawdb.DeleteStorageSnapshot(batch, accountHash, storageHash)
				base.cache.Set(append(accountHash[:], storageHash[:]...), nil)
			}
			snapshotFlushStorageItemMeter.Mark(1)
			snapshotFlushStorageSizeMeter.Mark(int64(len(data)))
		}
	}
	// Update the snapshot block marker and write any remainder data
	customrawdb.WriteSnapshotBlockHash(batch, bottom.blockHash)
	rawdb.WriteSnapshotRoot(batch, bottom.root)

	// Write out the generator progress marker and report
	journalProgress(batch, base.genMarker, base.genStats)

	// Flush all the updates in the single db operation. Ensure the
	// disk layer transition is atomic.
	if err := batch.Write(); err != nil {
		log.Crit("Failed to write leftover snapshot", "err", err)
	}
	log.Debug("Journalled disk layer", "root", bottom.root, "complete", base.genMarker == nil)
	res := &diskLayer{
		root:       bottom.root,
		blockHash:  bottom.blockHash,
		cache:      base.cache,
		diskdb:     base.diskdb,
		triedb:     base.triedb,
		genMarker:  base.genMarker,
		genPending: base.genPending,
		created:    time.Now(),
	}
	// If snapshot generation hasn't finished yet, port over all the starts and
	// continue where the previous round left off.
	//
	// Note, the `base.genAbort` comparison is not used normally, it's checked
	// to allow the tests to play with the marker without triggering this path.
	if base.genMarker != nil && base.genAbort != nil {
		res.genMarker = base.genMarker
		res.genAbort = make(chan chan struct{})

		// If the diskLayer we are about to discard is not very old, we skip
		// generation on the next layer (assuming generation will just get canceled
		// before doing meaningful work anyways).
		diskLayerAge := base.abortStarted.Sub(base.created)
		if diskLayerAge < skipGenThreshold {
			log.Debug("Skipping snapshot generation", "previous disk layer age", diskLayerAge)
			res.genStats = base.genStats
		} else {
			go res.generate(base.genStats)
		}
	}
	return res, base.genMarker == nil, nil
}

// Release releases resources
func (t *Tree) Release() {
	if dl := t.disklayer(); dl != nil {
		dl.Release()
	}
}

// Rebuild wipes all available snapshot data from the persistent database and
// discard all caches and diff layers. Afterwards, it starts a new snapshot
// generator with the given root hash.
func (t *Tree) Rebuild(blockHash, root common.Hash) {
	t.lock.Lock()
	defer t.lock.Unlock()

	// Track whether there's a wipe currently running and keep it alive if so
	var wiper chan struct{}

	// Iterate over and mark all layers stale
	for _, layer := range t.blockLayers {
		switch layer := layer.(type) {
		case *diskLayer:
			// If the base layer is generating, abort it and save
			if layer.genAbort != nil {
				abort := make(chan struct{})
				layer.genAbort <- abort
				<-abort

				if stats := layer.genStats; stats != nil {
					wiper = stats.wiping
				}
			}
			// Layer should be inactive now, mark it as stale
			layer.lock.Lock()
			layer.stale = true
			layer.lock.Unlock()

		case *diffLayer:
			// If the layer is a simple diff, simply mark as stale
			layer.lock.Lock()
			layer.stale.Store(true)
			layer.lock.Unlock()

		default:
			panic(fmt.Sprintf("unknown layer type: %T", layer))
		}
	}
	// Start generating a new snapshot from scratch on a background thread. The
	// generator will run a wiper first if there's not one running right now.
	log.Info("Rebuilding state snapshot")
	base := generateSnapshot(t.diskdb, t.triedb, t.config.CacheSize, blockHash, root, wiper)
	t.blockLayers = map[common.Hash]snapshot{
		blockHash: base,
	}
	t.stateLayers = map[common.Hash]map[common.Hash]snapshot{
		root: {
			blockHash: base,
		},
	}
}

// AccountIterator creates a new account iterator for the specified root hash and
// seeks to a starting account hash. When [force] is true, a new account
// iterator is created without acquiring the [snapTree] lock and without
// confirming that the snapshot on the disk layer is fully generated.
func (t *Tree) AccountIterator(root common.Hash, seek common.Hash, force bool) (AccountIterator, error) {
	if !force {
		ok, err := t.generating()
		if err != nil {
			return nil, err
		}
		if ok {
			return nil, ErrNotConstructed
		}
	}
	return newFastAccountIterator(t, root, seek, force)
}

// StorageIterator creates a new storage iterator for the specified root hash and
// account. The iterator will be move to the specific start position. When [force]
// is true, a new account iterator is created without acquiring the [snapTree]
// lock and without confirming that the snapshot on the disk layer is fully generated.
func (t *Tree) StorageIterator(root common.Hash, account common.Hash, seek common.Hash) (StorageIterator, error) {
	return t.StorageIteratorWithForce(root, account, seek, false)
}

func (t *Tree) StorageIteratorWithForce(root common.Hash, account common.Hash, seek common.Hash, force bool) (StorageIterator, error) {
	if !force {
		ok, err := t.generating()
		if err != nil {
			return nil, err
		}
		if ok {
			return nil, ErrNotConstructed
		}
	}
	return newFastStorageIterator(t, root, account, seek, force)
}

// Verify iterates the whole state(all the accounts as well as the corresponding storages)
// with the specific root and compares the re-computed hash with the original one.
func (t *Tree) Verify(root common.Hash) error {
	return t.verify(root, false)
}

// verify iterates the whole state(all the accounts as well as the corresponding storages)
// with the specific root and compares the re-computed hash with the original one.
// When [force] is true, it is assumed that the caller has confirmed that the
// snapshot is generated and that they hold the snapTree lock.
func (t *Tree) verify(root common.Hash, force bool) error {
	acctIt, err := t.AccountIterator(root, common.Hash{}, force)
	if err != nil {
		return err
	}
	defer acctIt.Release()

	got, err := generateTrieRoot(nil, "", acctIt, common.Hash{}, stackTrieGenerate, func(db ethdb.KeyValueWriter, accountHash, codeHash common.Hash, stat *generateStats) (common.Hash, error) {
		storageIt, err := t.StorageIteratorWithForce(root, accountHash, common.Hash{}, force)
		if err != nil {
			return common.Hash{}, err
		}
		defer storageIt.Release()

		hash, err := generateTrieRoot(nil, "", storageIt, accountHash, stackTrieGenerate, nil, stat, false)
		if err != nil {
			return common.Hash{}, err
		}
		return hash, nil
	}, newGenerateStats(), true)

	if err != nil {
		return err
	}
	if got != root {
		return fmt.Errorf("state root hash mismatch: got %x, want %x", got, root)
	}
	return nil
}

// disklayer is an internal helper function to return the disk layer.
// The lock of snapTree is assumed to be held already.
func (t *Tree) disklayer() *diskLayer {
	var snap snapshot
	for _, s := range t.blockLayers {
		snap = s
		break
	}
	if snap == nil {
		return nil
	}
	switch layer := snap.(type) {
	case *diskLayer:
		return layer
	case *diffLayer:
		layer.lock.RLock()
		defer layer.lock.RUnlock()
		return layer.origin
	default:
		panic(fmt.Sprintf("%T: undefined layer", snap))
	}
}

// diskRoot is a internal helper function to return the disk layer root.
// The lock of snapTree is assumed to be held already.
func (t *Tree) diskRoot() common.Hash {
	disklayer := t.disklayer()
	if disklayer == nil {
		return common.Hash{}
	}
	return disklayer.Root()
}

// generating is an internal helper function which reports whether the snapshot
// is still under the construction.
func (t *Tree) generating() (bool, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	layer := t.disklayer()
	if layer == nil {
		return false, errors.New("disk layer is missing")
	}
	layer.lock.RLock()
	defer layer.lock.RUnlock()
	return layer.genMarker != nil, nil
}

// DiskRoot is an external helper function to return the disk layer root.
func (t *Tree) DiskRoot() common.Hash {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.diskRoot()
}

// Size returns the memory usage of the diff layers above the disk layer and the
// dirty nodes buffered in the disk layer. Currently, the implementation uses a
// special diff layer (the first) as an aggregator simulating a dirty buffer, so
// the second return will always be 0. However, this will be made consistent with
// the pathdb, which will require a second return.
func (t *Tree) Size() (diffs common.StorageSize, buf common.StorageSize) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	var size common.StorageSize
	for _, layer := range t.blockLayers {
		if layer, ok := layer.(*diffLayer); ok {
			size += common.StorageSize(layer.memory)
		}
	}
	return size, 0
}
