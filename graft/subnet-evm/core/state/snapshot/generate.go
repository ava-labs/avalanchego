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

package snapshot

import (
	"bytes"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/graft/evm/utils"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
)

const (
	snapshotCacheNamespace            = "state/snapshot/clean/fastcache" // prefix for detailed stats from the snapshot fastcache
	snapshotCacheStatsUpdateFrequency = 1000                             // update stats from the snapshot fastcache once per 1000 ops
)

// generateSnapshot regenerates a brand new snapshot based on an existing state
// database and head block asynchronously. The snapshot is returned immediately
// and generation is continued in the background until done.
func generateSnapshot(diskdb ethdb.KeyValueStore, triedb *triedb.Database, cache int, blockHash, root common.Hash, wiper chan struct{}) *diskLayer {
	// Wipe any previously existing snapshot from the database if no wiper is
	// currently in progress.
	if wiper == nil {
		wiper = WipeSnapshot(diskdb, true)
	}
	// Create a new disk layer with an initialized state marker at zero
	var (
		stats     = &generatorStats{wiping: wiper, start: time.Now()}
		batch     = diskdb.NewBatch()
		genMarker = []byte{} // Initialized but empty!
	)
	customrawdb.WriteSnapshotBlockHash(batch, blockHash)
	rawdb.WriteSnapshotRoot(batch, root)
	journalProgress(batch, genMarker, stats)
	if err := batch.Write(); err != nil {
		log.Crit("Failed to write initialized state marker", "err", err)
	}
	base := &diskLayer{
		diskdb:     diskdb,
		triedb:     triedb,
		blockHash:  blockHash,
		root:       root,
		cache:      newMeteredSnapshotCache(cache * 1024 * 1024),
		genMarker:  genMarker,
		genPending: make(chan struct{}),
		genAbort:   make(chan chan struct{}),
		created:    time.Now(),
	}
	go base.generate(stats)
	log.Debug("Start snapshot generation", "root", root)
	return base
}

// journalProgress persists the generator stats into the database to resume later.
func journalProgress(db ethdb.KeyValueWriter, marker []byte, stats *generatorStats) {
	// Write out the generator marker. Note it's a standalone disk layer generator
	// which is not mixed with journal. It's ok if the generator is persisted while
	// journal is not.
	entry := journalGenerator{
		Done:   marker == nil,
		Marker: marker,
	}
	if stats != nil {
		entry.Wiping = (stats.wiping != nil)
		entry.Accounts = stats.accounts
		entry.Slots = stats.slots
		entry.Storage = uint64(stats.storage)
	}
	blob, err := rlp.EncodeToBytes(entry)
	if err != nil {
		panic(err) // Cannot happen, here to catch dev errors
	}
	var logstr string
	switch {
	case marker == nil:
		logstr = "done"
	case bytes.Equal(marker, []byte{}):
		logstr = "empty"
	case len(marker) == common.HashLength:
		logstr = fmt.Sprintf("%#x", marker)
	default:
		logstr = fmt.Sprintf("%#x:%#x", marker[:common.HashLength], marker[common.HashLength:])
	}
	log.Debug("Journalled generator progress", "progress", logstr)
	rawdb.WriteSnapshotGenerator(db, blob)
}

// checkAndFlush checks to see if snapshot generation has been aborted or if
// the current batch size is greater than ethdb.IdealBatchSize. If so, it saves
// the current progress to disk and returns true. Else, it could log current
// progress and returns true.
func (dl *diskLayer) checkAndFlush(batch ethdb.Batch, stats *generatorStats, currentLocation []byte) bool {
	// If we've exceeded our batch allowance or termination was requested, flush to disk
	var abort chan struct{}
	select {
	case abort = <-dl.genAbort:
	default:
	}
	if batch.ValueSize() > ethdb.IdealBatchSize || abort != nil {
		if bytes.Compare(currentLocation, dl.genMarker) < 0 {
			log.Error("Snapshot generator went backwards",
				"currentLocation", fmt.Sprintf("%x", currentLocation),
				"genMarker", fmt.Sprintf("%x", dl.genMarker))
		}
		// Flush out the batch anyway no matter it's empty or not.
		// It's possible that all the states are recovered and the
		// generation indeed makes progress.
		journalProgress(batch, currentLocation, stats)

		if err := batch.Write(); err != nil {
			log.Error("Failed to flush batch", "err", err)
			if abort == nil {
				abort = <-dl.genAbort
			}
			dl.genStats = stats
			close(abort)
			return true
		}
		batch.Reset()

		dl.lock.Lock()
		dl.genMarker = currentLocation
		dl.lock.Unlock()

		if abort != nil {
			stats.Debug("Aborting state snapshot generation", dl.root, currentLocation)
			dl.genStats = stats
			close(abort)
			return true
		}
	}
	if time.Since(dl.logged) > 8*time.Second {
		stats.Info("Generating state snapshot", dl.root, currentLocation)
		dl.logged = time.Now()
	}
	return false
}

// generate is a background thread that iterates over the state and storage tries,
// constructing the state snapshot. All the arguments are purely for statistics
// gathering and logging, since the method surfs the blocks as they arrive, often
// being restarted.
func (dl *diskLayer) generate(stats *generatorStats) {
	// If a database wipe is in operation, wait until it's done
	if stats.wiping != nil {
		stats.Info("Wiper running, state snapshotting paused", common.Hash{}, dl.genMarker)
		select {
		// If wiper is done, resume normal mode of operation
		case <-stats.wiping:
			stats.wiping = nil
			stats.start = time.Now()

		// If generator was aborted during wipe, return
		case abort := <-dl.genAbort:
			stats.Debug("Aborting state snapshot generation", dl.root, dl.genMarker)
			dl.genStats = stats
			close(abort)
			return
		}
	}
	// Create an account and state iterator pointing to the current generator marker
	trieId := trie.StateTrieID(dl.root)
	accTrie, err := trie.NewStateTrie(trieId, dl.triedb)
	if err != nil {
		// The account trie is missing (GC), surf the chain until one becomes available
		stats.Info("Trie missing, state snapshotting paused", dl.root, dl.genMarker)
		abort := <-dl.genAbort
		dl.genStats = stats
		close(abort)
		return
	}
	stats.Debug("Resuming state snapshot generation", dl.root, dl.genMarker)

	var accMarker []byte
	if len(dl.genMarker) > 0 { // []byte{} is the start, use nil for that
		accMarker = dl.genMarker[:common.HashLength]
	}
	nodeIt, err := accTrie.NodeIterator(accMarker)
	if err != nil {
		log.Error("Generator failed to create account iterator", "root", dl)
		abort := <-dl.genAbort
		dl.genStats = stats
		close(abort)
		return
	}
	accIt := trie.NewIterator(nodeIt)
	batch := dl.diskdb.NewBatch()

	// Iterate from the previous marker and continue generating the state snapshot
	dl.logged = time.Now()
	for accIt.Next() {
		// Retrieve the current account and flatten it into the internal format
		accountHash := common.BytesToHash(accIt.Key)

		var acc types.StateAccount
		if err := rlp.DecodeBytes(accIt.Value, &acc); err != nil {
			log.Crit("Invalid account encountered during snapshot creation", "err", err)
		}
		data := types.SlimAccountRLP(acc)

		// If the account is not yet in-progress, write it out
		if accMarker == nil || !bytes.Equal(accountHash[:], accMarker) {
			rawdb.WriteAccountSnapshot(batch, accountHash, data)
			stats.storage += common.StorageSize(1 + common.HashLength + len(data))
			stats.accounts++
		}
		marker := accountHash[:]
		// If the snap generation goes here after interrupted, genMarker may go backward
		// when last genMarker is consisted of accountHash and storageHash
		if accMarker != nil && bytes.Equal(marker, accMarker) && len(dl.genMarker) > common.HashLength {
			marker = dl.genMarker[:]
		}
		if dl.checkAndFlush(batch, stats, marker) {
			// checkAndFlush handles abort
			return
		}
		// If the iterated account is a contract, iterate through corresponding contract
		// storage to generate snapshot entries.
		if acc.Root != types.EmptyRootHash {
			storeTrieId := trie.StorageTrieID(dl.root, accountHash, acc.Root)
			storeTrie, err := trie.NewStateTrie(storeTrieId, dl.triedb)
			if err != nil {
				log.Error("Generator failed to access storage trie", "root", dl.root, "account", accountHash, "stroot", acc.Root, "err", err)
				abort := <-dl.genAbort
				dl.genStats = stats
				close(abort)
				return
			}
			var storeMarker []byte
			if accMarker != nil && bytes.Equal(accountHash[:], accMarker) && len(dl.genMarker) > common.HashLength {
				storeMarker = dl.genMarker[common.HashLength:]
			}
			nodeIt, err := storeTrie.NodeIterator(storeMarker)
			if err != nil {
				log.Error("Generator failed to create storage iterator", "root", dl.root, "account", accountHash, "stroot", acc.Root, "err", err)
				abort := <-dl.genAbort
				dl.genStats = stats
				close(abort)
				return
			}
			storeIt := trie.NewIterator(nodeIt)
			for storeIt.Next() {
				rawdb.WriteStorageSnapshot(batch, accountHash, common.BytesToHash(storeIt.Key), storeIt.Value)
				stats.storage += common.StorageSize(1 + 2*common.HashLength + len(storeIt.Value))
				stats.slots++

				if dl.checkAndFlush(batch, stats, append(accountHash[:], storeIt.Key...)) {
					// checkAndFlush handles abort
					return
				}
			}
			if err := storeIt.Err; err != nil {
				log.Error("Generator failed to iterate storage trie", "accroot", dl.root, "acchash", common.BytesToHash(accIt.Key), "stroot", acc.Root, "err", err)
				abort := <-dl.genAbort
				dl.genStats = stats
				close(abort)
				return
			}
		}
		if time.Since(dl.logged) > 8*time.Second {
			stats.Info("Generating state snapshot", dl.root, accIt.Key)
			dl.logged = time.Now()
		}
		// Some account processed, unmark the marker
		accMarker = nil
	}
	if err := accIt.Err; err != nil {
		log.Error("Generator failed to iterate account trie", "root", dl.root, "err", err)
		abort := <-dl.genAbort
		dl.genStats = stats
		close(abort)
		return
	}
	// Snapshot fully generated, set the marker to nil.
	// Note even there is nothing to commit, persist the
	// generator anyway to mark the snapshot is complete.
	journalProgress(batch, nil, stats)
	if err := batch.Write(); err != nil {
		log.Error("Failed to flush batch", "err", err)
		abort := <-dl.genAbort
		dl.genStats = stats
		close(abort)
		return
	}

	log.Info("Generated state snapshot", "accounts", stats.accounts, "slots", stats.slots,
		"storage", stats.storage, "elapsed", common.PrettyDuration(time.Since(stats.start)))

	dl.lock.Lock()
	dl.genMarker = nil
	dl.genStats = stats
	close(dl.genPending)
	dl.lock.Unlock()

	// Someone will be looking for us, wait it out
	abort := <-dl.genAbort
	close(abort)
}

func newMeteredSnapshotCache(size int) *utils.MeteredCache {
	return utils.NewMeteredCache(size, snapshotCacheNamespace, snapshotCacheStatsUpdateFrequency)
}
