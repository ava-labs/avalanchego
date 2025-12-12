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
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customrawdb"
)

// journalGenerator is a disk layer entry containing the generator progress marker.
type journalGenerator struct {
	// Indicator that whether the database was in progress of being wiped.
	// It's deprecated but keep it here for background compatibility.
	Wiping bool

	Done     bool // Whether the generator finished creating the snapshot
	Marker   []byte
	Accounts uint64
	Slots    uint64
	Storage  uint64
}

// loadSnapshot loads a pre-existing state snapshot backed by a key-value
// store. If loading the snapshot from disk is successful, this function also
// returns a boolean indicating whether or not the snapshot is fully generated.
func loadSnapshot(diskdb ethdb.KeyValueStore, triedb *triedb.Database, cache int, blockHash, root common.Hash, noBuild bool) (snapshot, bool, error) {
	// Retrieve the block number and hash of the snapshot, failing if no snapshot
	// is present in the database (or crashed mid-update).
	baseBlockHash := customrawdb.ReadSnapshotBlockHash(diskdb)
	if baseBlockHash == (common.Hash{}) {
		return nil, false, errors.New("missing or corrupted snapshot, no snapshot block hash")
	}
	if baseBlockHash != blockHash {
		return nil, false, fmt.Errorf("block hash stored on disk (%#x) does not match last accepted (%#x)", baseBlockHash, blockHash)
	}
	baseRoot := rawdb.ReadSnapshotRoot(diskdb)
	if baseRoot == (common.Hash{}) {
		return nil, false, errors.New("missing or corrupted snapshot, no snapshot root")
	}
	if baseRoot != root {
		return nil, false, fmt.Errorf("root stored on disk (%#x) does not match last accepted (%#x)", baseRoot, root)
	}

	// Retrieve the disk layer generator. It must exist, no matter the
	// snapshot is fully generated or not. Otherwise the entire disk
	// layer is invalid.
	generatorBlob := rawdb.ReadSnapshotGenerator(diskdb)
	if len(generatorBlob) == 0 {
		return nil, false, errors.New("missing snapshot generator")
	}
	var generator journalGenerator
	if err := rlp.DecodeBytes(generatorBlob, &generator); err != nil {
		return nil, false, fmt.Errorf("failed to decode snapshot generator: %v", err)
	}

	// Instantiate snapshot as disk layer with last recorded block hash and root
	snapshot := &diskLayer{
		diskdb:    diskdb,
		triedb:    triedb,
		cache:     newMeteredSnapshotCache(cache * 1024 * 1024),
		root:      baseRoot,
		blockHash: baseBlockHash,
		created:   time.Now(),
	}

	var wiper chan struct{}
	// Load the disk layer status from the generator if it's not complete
	if !generator.Done {
		// If the generator was still wiping, restart one from scratch (fine for
		// now as it's rare and the wiper deletes the stuff it touches anyway, so
		// restarting won't incur a lot of extra database hops.
		if generator.Wiping {
			log.Info("Resuming previous snapshot wipe")
			wiper = WipeSnapshot(diskdb, false)
		}
		// Whether or not wiping was in progress, load any generator progress too
		snapshot.genMarker = generator.Marker
		if snapshot.genMarker == nil {
			snapshot.genMarker = []byte{}
		}
	}

	// Everything loaded correctly, resume any suspended operations
	// if the background generation is allowed
	if !generator.Done && !noBuild {
		snapshot.genPending = make(chan struct{})
		snapshot.genAbort = make(chan chan struct{})

		var origin uint64
		if len(generator.Marker) >= 8 {
			origin = binary.BigEndian.Uint64(generator.Marker)
		}
		go snapshot.generate(&generatorStats{
			wiping:   wiper,
			origin:   origin,
			start:    time.Now(),
			accounts: generator.Accounts,
			slots:    generator.Slots,
			storage:  common.StorageSize(generator.Storage),
		})
	}

	return snapshot, generator.Done, nil
}

// ResetSnapshotGeneration writes a clean snapshot generator marker to [db]
// so no re-generation is performed after.
func ResetSnapshotGeneration(db ethdb.KeyValueWriter) {
	journalProgress(db, nil, nil)
}
