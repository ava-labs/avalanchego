// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	"time"

	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

// WipeSnapshot starts a goroutine to iterate over the entire key-value database
// and delete all the data associated with the snapshot (accounts, storage,
// metadata). After all is done, the snapshot range of the database is compacted
// to free up unused data blocks.
func WipeSnapshot(db ethdb.KeyValueStore, full bool) chan struct{} {
	// Wipe the snapshot root marker synchronously
	if full {
		customrawdb.DeleteSnapshotBlockHash(db)
		rawdb.DeleteSnapshotRoot(db)
	}
	// Wipe everything else asynchronously
	wiper := make(chan struct{}, 1)
	go func() {
		if err := wipeContent(db); err != nil {
			log.Error("Failed to wipe state snapshot", "err", err) // Database close will trigger this
			return
		}
		close(wiper)
	}()
	return wiper
}

// wipeContent iterates over the entire key-value database and deletes all the
// data associated with the snapshot (accounts, storage), but not the root hash
// as the wiper is meant to run on a background thread but the root needs to be
// removed in sync to avoid data races. After all is done, the snapshot range of
// the database is compacted to free up unused data blocks.
func wipeContent(db ethdb.KeyValueStore) error {
	if err := wipeKeyRange(db, "accounts", rawdb.SnapshotAccountPrefix, nil, nil, len(rawdb.SnapshotAccountPrefix)+common.HashLength, true); err != nil {
		return err
	}
	if err := wipeKeyRange(db, "storage", rawdb.SnapshotStoragePrefix, nil, nil, len(rawdb.SnapshotStoragePrefix)+2*common.HashLength, true); err != nil {
		return err
	}

	return nil
}

// wipeKeyRange deletes a range of keys from the database starting with prefix
// and having a specific total key length. The start and limit is optional for
// specifying a particular key range for deletion.
//
// Origin is included for wiping and limit is excluded if they are specified.
func wipeKeyRange(db ethdb.KeyValueStore, kind string, prefix []byte, origin []byte, limit []byte, keylen int, report bool) error {
	// Batch deletions together to avoid holding an iterator for too long
	var (
		batch = db.NewBatch()
		items int
	)
	// Iterate over the key-range and delete all of them
	start, logged := time.Now(), time.Now()

	it := db.NewIterator(prefix, origin)
	var stop []byte
	if limit != nil {
		stop = append(prefix, limit...)
	}
	for it.Next() {
		// Skip any keys with the correct prefix but wrong length (trie nodes)
		key := it.Key()
		if !bytes.HasPrefix(key, prefix) {
			break
		}
		if len(key) != keylen {
			continue
		}
		if stop != nil && bytes.Compare(key, stop) >= 0 {
			break
		}
		// Delete the key and periodically recreate the batch and iterator
		batch.Delete(key)
		items++

		if items%10000 == 0 {
			// Batch too large (or iterator too long lived, flush and recreate)
			it.Release()
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
			seekPos := key[len(prefix):]
			it = db.NewIterator(prefix, seekPos)

			if time.Since(logged) > 8*time.Second && report {
				log.Info("Deleting state snapshot leftovers", "kind", kind, "wiped", items, "elapsed", common.PrettyDuration(time.Since(start)))
				logged = time.Now()
			}
		}
	}
	it.Release()
	if err := batch.Write(); err != nil {
		return err
	}
	if report {
		log.Info("Deleted state snapshot leftovers", "kind", kind, "wiped", items, "elapsed", common.PrettyDuration(time.Since(start)))
	}
	return nil
}
