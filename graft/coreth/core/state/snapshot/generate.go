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
	"encoding/binary"
	"fmt"
	"math/big"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/ethdb"
	"github.com/ava-labs/coreth/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	// emptyRoot is the known root hash of an empty trie.
	emptyRoot = common.HexToHash("56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421")

	// emptyCode is the known hash of the empty EVM bytecode.
	emptyCode = crypto.Keccak256Hash(nil)
)

// generatorStats is a collection of statistics gathered by the snapshot generator
// for logging purposes.
type generatorStats struct {
	wiping   chan struct{}      // Notification channel if wiping is in progress
	origin   uint64             // Origin prefix where generation started
	start    time.Time          // Timestamp when generation started
	accounts uint64             // Number of accounts indexed(generated or recovered)
	slots    uint64             // Number of storage slots indexed(generated or recovered)
	storage  common.StorageSize // Total account and storage slot size(generation or recovery)
}

// Info creates an contextual info-level log with the given message and the context pulled
// from the internally maintained statistics.
func (gs *generatorStats) Info(msg string, root common.Hash, marker []byte) {
	gs.log(log.LvlInfo, msg, root, marker)
}

// Debug creates an contextual debug-level log with the given message and the context pulled
// from the internally maintained statistics.
func (gs *generatorStats) Debug(msg string, root common.Hash, marker []byte) {
	gs.log(log.LvlDebug, msg, root, marker)
}

// log creates an contextual log with the given message and the context pulled
// from the internally maintained statistics.
func (gs *generatorStats) log(level log.Lvl, msg string, root common.Hash, marker []byte) {
	var ctx []interface{}
	if root != (common.Hash{}) {
		ctx = append(ctx, []interface{}{"root", root}...)
	}
	// Figure out whether we're after or within an account
	switch len(marker) {
	case common.HashLength:
		ctx = append(ctx, []interface{}{"at", common.BytesToHash(marker)}...)
	case 2 * common.HashLength:
		ctx = append(ctx, []interface{}{
			"in", common.BytesToHash(marker[:common.HashLength]),
			"at", common.BytesToHash(marker[common.HashLength:]),
		}...)
	}
	// Add the usual measurements
	ctx = append(ctx, []interface{}{
		"accounts", gs.accounts,
		"slots", gs.slots,
		"storage", gs.storage,
		"elapsed", common.PrettyDuration(time.Since(gs.start)),
	}...)
	// Calculate the estimated indexing time based on current stats
	if len(marker) > 0 {
		if done := binary.BigEndian.Uint64(marker[:8]) - gs.origin; done > 0 {
			left := math.MaxUint64 - binary.BigEndian.Uint64(marker[:8])

			speed := done/uint64(time.Since(gs.start)/time.Millisecond+1) + 1 // +1s to avoid division by zero
			ctx = append(ctx, []interface{}{
				"eta", common.PrettyDuration(time.Duration(left/speed) * time.Millisecond),
			}...)
		}
	}

	switch level {
	case log.LvlTrace:
		log.Trace(msg, ctx...)
	case log.LvlDebug:
		log.Debug(msg, ctx...)
	case log.LvlInfo:
		log.Info(msg, ctx...)
	case log.LvlWarn:
		log.Warn(msg, ctx...)
	case log.LvlError:
		log.Error(msg, ctx...)
	case log.LvlCrit:
		log.Crit(msg, ctx...)
	default:
		log.Error(fmt.Sprintf("log with invalid log level %s: %s", level, msg), ctx...)
	}
}

// generateSnapshot regenerates a brand new snapshot based on an existing state
// database and head block asynchronously. The snapshot is returned immediately
// and generation is continued in the background until done.
func generateSnapshot(diskdb ethdb.KeyValueStore, triedb *trie.Database, cache int, blockHash, root common.Hash, wiper chan struct{}) *diskLayer {
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
	rawdb.WriteSnapshotBlockHash(batch, blockHash)
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
		cache:      fastcache.New(cache * 1024 * 1024),
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
	accTrie, err := trie.NewSecure(dl.root, dl.triedb)
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
	accIt := trie.NewIterator(accTrie.NodeIterator(accMarker))
	batch := dl.diskdb.NewBatch()

	// Iterate from the previous marker and continue generating the state snapshot
	dl.logged = time.Now()
	for accIt.Next() {
		// Retrieve the current account and flatten it into the internal format
		accountHash := common.BytesToHash(accIt.Key)

		var acc struct {
			Nonce       uint64
			Balance     *big.Int
			Root        common.Hash
			CodeHash    []byte
			IsMultiCoin bool
		}
		if err := rlp.DecodeBytes(accIt.Value, &acc); err != nil {
			log.Crit("Invalid account encountered during snapshot creation", "err", err)
		}
		data := SlimAccountRLP(acc.Nonce, acc.Balance, acc.Root, acc.CodeHash, acc.IsMultiCoin)

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
		if acc.Root != emptyRoot {
			storeTrie, err := trie.NewSecure(acc.Root, dl.triedb)
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
			storeIt := trie.NewIterator(storeTrie.NodeIterator(storeMarker))
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
