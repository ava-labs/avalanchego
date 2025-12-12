// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
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
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
)

// Tests that given a database with random data content, all parts of a snapshot
// can be crrectly wiped without touching anything else.
func TestWipe(t *testing.T) {
	// Create a database with some random snapshot data
	db := memorydb.New()
	for i := 0; i < 128; i++ {
		rawdb.WriteAccountSnapshot(db, randomHash(), randomHash().Bytes())
	}
	customrawdb.WriteSnapshotBlockHash(db, randomHash())
	rawdb.WriteSnapshotRoot(db, randomHash())

	// Add some random non-snapshot data too to make wiping harder
	for i := 0; i < 500; i++ {
		// Generate keys with wrong length for a state snapshot item
		keysuffix := make([]byte, 31)
		rand.Read(keysuffix)
		db.Put(append(rawdb.SnapshotAccountPrefix, keysuffix...), randomHash().Bytes())
		keysuffix = make([]byte, 33)
		rand.Read(keysuffix)
		db.Put(append(rawdb.SnapshotAccountPrefix, keysuffix...), randomHash().Bytes())
	}
	count := func() (items int) {
		it := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
		defer it.Release()
		for it.Next() {
			if len(it.Key()) == len(rawdb.SnapshotAccountPrefix)+common.HashLength {
				items++
			}
		}
		return items
	}
	// Sanity check that all the keys are present
	if items := count(); items != 128 {
		t.Fatalf("snapshot size mismatch: have %d, want %d", items, 128)
	}
	if hash := customrawdb.ReadSnapshotBlockHash(db); hash == (common.Hash{}) {
		t.Errorf("snapshot block hash marker mismatch: have %#x, want <not-nil>", hash)
	}
	if hash := rawdb.ReadSnapshotRoot(db); hash == (common.Hash{}) {
		t.Errorf("snapshot block root marker mismatch: have %#x, want <not-nil>", hash)
	}
	// Wipe all snapshot entries from the database
	<-WipeSnapshot(db, true)

	// Iterate over the database end ensure no snapshot information remains
	if items := count(); items != 0 {
		t.Fatalf("snapshot size mismatch: have %d, want %d", items, 0)
	}
	// Iterate over the database and ensure miscellaneous items are present
	items := 0
	it := db.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		items++
	}
	if items != 1000 {
		t.Fatalf("misc item count mismatch: have %d, want %d", items, 1000)
	}

	if hash := customrawdb.ReadSnapshotBlockHash(db); hash != (common.Hash{}) {
		t.Errorf("snapshot block hash marker remained after wipe: %#x", hash)
	}
	if hash := rawdb.ReadSnapshotRoot(db); hash != (common.Hash{}) {
		t.Errorf("snapshot block root marker remained after wipe: %#x", hash)
	}
}
