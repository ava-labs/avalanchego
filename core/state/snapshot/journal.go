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
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/VictoriaMetrics/fastcache"
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

const journalVersion uint64 = 0

// journalGenerator is a disk layer entry containing the generator progress marker.
type journalGenerator struct {
	// Indicator that whether the database was in progress of being wiped.
	// It's deprecated but keep it here for background compatibility.
	Wiping   bool
	Done     bool // Whether the generator finished creating the snapshot
	Marker   []byte
	Accounts uint64
	Slots    uint64
	Storage  uint64
}

// loadSnapshot loads a pre-existing state snapshot backed by a key-value
// store. If loading the snapshot from disk is successful, this function also
// returns a boolean indicating whether or not the snapshot is fully generated.
func loadSnapshot(diskdb ethdb.KeyValueStore, triedb *trie.Database, cache int, blockHash, root common.Hash, recovery bool) (snapshot, bool, error) {
	// Retrieve the block number and hash of the snapshot, failing if no snapshot
	// is present in the database (or crashed mid-update).
	baseBlockHash := rawdb.ReadSnapshotBlockHash(diskdb)
	if baseBlockHash == (common.Hash{}) {
		return nil, false, fmt.Errorf("missing or corrupted snapshot, no snapshot block hash")
	}
	baseRoot := rawdb.ReadSnapshotRoot(diskdb)
	if baseRoot == (common.Hash{}) {
		return nil, false, errors.New("missing or corrupted snapshot, no snapshot root")
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

	snapshot := &diskLayer{
		diskdb:    diskdb,
		triedb:    triedb,
		cache:     fastcache.New(cache * 1024 * 1024),
		root:      baseRoot,
		blockHash: baseBlockHash,
		created:   time.Now(),
	}
	// Entire snapshot journal loaded, sanity check the head. If the loaded
	// snapshot is not matched with current state root, print a warning log
	// or discard the entire snapshot it's legacy snapshot.
	//
	// Possible scenario: Geth was crashed without persisting journal and then
	// restart, the head is rewound to the point with available state(trie)
	// which is below the snapshot. In this case the snapshot can be recovered
	// by re-executing blocks but right now it's unavailable.
	if head := snapshot.Root(); head != root {
		// If it's legacy snapshot, or it's new-format snapshot but
		// it's not in recovery mode, returns the error here for
		// rebuilding the entire snapshot forcibly.
		if !recovery {
			return nil, false, fmt.Errorf("head doesn't match snapshot: have %#x, want %#x", head, root)
		}
		// It's in snapshot recovery, the assumption is held that
		// the disk layer is always higher than chain head. It can
		// be eventually recovered when the chain head beyonds the
		// disk layer.
		log.Warn("Snapshot is not continuous with chain", "snaproot", head, "chainroot", root)
	}
	if headBlockHash := snapshot.BlockHash(); headBlockHash != blockHash {
		// If it's legacy snapshot, or it's new-format snapshot but
		// it's not in recovery mode, returns the error here for
		// rebuilding the entire snapshot forcibly.
		if !recovery {
			return nil, false, fmt.Errorf("head block hash doesn't match snapshot: have %#x, want %#x", headBlockHash, blockHash)
		}
		// It's in snapshot recovery, the assumption is held that
		// the disk layer is always higher than chain head. It can
		// be eventually recovered when the chain head beyonds the
		// disk layer.
		log.Warn("Snapshot is not continuous with chain", "snapBlockHash", headBlockHash, "chainBlockHash", blockHash)
	}
	// Everything loaded correctly, resume any suspended operations
	if !generator.Done {
		// If the generator was still wiping, restart one from scratch (fine for
		// now as it's rare and the wiper deletes the stuff it touches anyway, so
		// restarting won't incur a lot of extra database hops.
		var wiper chan struct{}
		if generator.Wiping {
			log.Info("Resuming previous snapshot wipe")
			wiper = wipeSnapshot(diskdb, false)
		}
		// Whether or not wiping was in progress, load any generator progress too
		snapshot.genMarker = generator.Marker
		if snapshot.genMarker == nil {
			snapshot.genMarker = []byte{}
		}
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
