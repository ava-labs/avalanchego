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
// Copyright 2018 The go-ethereum Authors
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

package rawdb

import (
	"encoding/json"
	"time"

	"github.com/ava-labs/subnet-evm/params"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// ReadDatabaseVersion retrieves the version number of the database.
func ReadDatabaseVersion(db ethdb.KeyValueReader) *uint64 {
	var version uint64

	enc, _ := db.Get(databaseVersionKey)
	if len(enc) == 0 {
		return nil
	}
	if err := rlp.DecodeBytes(enc, &version); err != nil {
		return nil
	}

	return &version
}

// WriteDatabaseVersion stores the version number of the database
func WriteDatabaseVersion(db ethdb.KeyValueWriter, version uint64) {
	enc, err := rlp.EncodeToBytes(version)
	if err != nil {
		log.Crit("Failed to encode database version", "err", err)
	}
	if err = db.Put(databaseVersionKey, enc); err != nil {
		log.Crit("Failed to store the database version", "err", err)
	}
}

// ReadChainConfig retrieves the consensus settings based on the given genesis hash.
func ReadChainConfig(db ethdb.KeyValueReader, hash common.Hash) *params.ChainConfig {
	data, _ := db.Get(configKey(hash))
	if len(data) == 0 {
		return nil
	}
	var config params.ChainConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log.Error("Invalid chain config JSON", "hash", hash, "err", err)
		return nil
	}

	// Read the upgrade config for this chain config
	data, _ = db.Get(upgradeConfigKey(hash))
	if len(data) == 0 {
		return &config // return early if no upgrade config is found
	}
	if err := json.Unmarshal(data, &config.UpgradeConfig); err != nil {
		log.Error("Invalid upgrade config JSON", "err", err)
		return nil
	}

	return &config
}

// WriteChainConfig writes the chain config settings to the database.
func WriteChainConfig(db ethdb.KeyValueWriter, hash common.Hash, cfg *params.ChainConfig) {
	if cfg == nil {
		return
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		log.Crit("Failed to JSON encode chain config", "err", err)
	}
	if err := db.Put(configKey(hash), data); err != nil {
		log.Crit("Failed to store chain config", "err", err)
	}

	// Write the upgrade config for this chain config
	data, err = json.Marshal(cfg.UpgradeConfig)
	if err != nil {
		log.Crit("Failed to JSON encode upgrade config", "err", err)
	}
	if err := db.Put(upgradeConfigKey(hash), data); err != nil {
		log.Crit("Failed to store upgrade config", "err", err)
	}
}

// crashList is a list of unclean-shutdown-markers, for rlp-encoding to the
// database
type crashList struct {
	Discarded uint64   // how many ucs have we deleted
	Recent    []uint64 // unix timestamps of 10 latest unclean shutdowns
}

const crashesToKeep = 10

// PushUncleanShutdownMarker appends a new unclean shutdown marker and returns
// the previous data
// - a list of timestamps
// - a count of how many old unclean-shutdowns have been discarded
func PushUncleanShutdownMarker(db ethdb.KeyValueStore) ([]uint64, uint64, error) {
	var uncleanShutdowns crashList
	// Read old data
	if data, err := db.Get(uncleanShutdownKey); err != nil {
		log.Warn("Error reading unclean shutdown markers", "error", err)
	} else if err := rlp.DecodeBytes(data, &uncleanShutdowns); err != nil {
		return nil, 0, err
	}
	var discarded = uncleanShutdowns.Discarded
	var previous = make([]uint64, len(uncleanShutdowns.Recent))
	copy(previous, uncleanShutdowns.Recent)
	// Add a new (but cap it)
	uncleanShutdowns.Recent = append(uncleanShutdowns.Recent, uint64(time.Now().Unix()))
	if count := len(uncleanShutdowns.Recent); count > crashesToKeep+1 {
		numDel := count - (crashesToKeep + 1)
		uncleanShutdowns.Recent = uncleanShutdowns.Recent[numDel:]
		uncleanShutdowns.Discarded += uint64(numDel)
	}
	// And save it again
	data, _ := rlp.EncodeToBytes(uncleanShutdowns)
	if err := db.Put(uncleanShutdownKey, data); err != nil {
		log.Warn("Failed to write unclean-shutdown marker", "err", err)
		return nil, 0, err
	}
	return previous, discarded, nil
}

// PopUncleanShutdownMarker removes the last unclean shutdown marker
func PopUncleanShutdownMarker(db ethdb.KeyValueStore) {
	var uncleanShutdowns crashList
	// Read old data
	if data, err := db.Get(uncleanShutdownKey); err != nil {
		log.Warn("Error reading unclean shutdown markers", "error", err)
	} else if err := rlp.DecodeBytes(data, &uncleanShutdowns); err != nil {
		log.Error("Error decoding unclean shutdown markers", "error", err) // Should mos def _not_ happen
	}
	if l := len(uncleanShutdowns.Recent); l > 0 {
		uncleanShutdowns.Recent = uncleanShutdowns.Recent[:l-1]
	}
	data, _ := rlp.EncodeToBytes(uncleanShutdowns)
	if err := db.Put(uncleanShutdownKey, data); err != nil {
		log.Warn("Failed to clear unclean-shutdown marker", "err", err)
	}
}

// UpdateUncleanShutdownMarker updates the last marker's timestamp to now.
func UpdateUncleanShutdownMarker(db ethdb.KeyValueStore) {
	var uncleanShutdowns crashList
	// Read old data
	if data, err := db.Get(uncleanShutdownKey); err != nil {
		log.Warn("Error reading unclean shutdown markers", "error", err)
	} else if err := rlp.DecodeBytes(data, &uncleanShutdowns); err != nil {
		log.Warn("Error decoding unclean shutdown markers", "error", err)
	}
	// This shouldn't happen because we push a marker on Backend instantiation
	count := len(uncleanShutdowns.Recent)
	if count == 0 {
		log.Warn("No unclean shutdown marker to update")
		return
	}
	uncleanShutdowns.Recent[count-1] = uint64(time.Now().Unix())
	data, _ := rlp.EncodeToBytes(uncleanShutdowns)
	if err := db.Put(uncleanShutdownKey, data); err != nil {
		log.Warn("Failed to write unclean-shutdown marker", "err", err)
	}
}

// WriteTimeMarker writes a marker of the current time in the db at [key]
func WriteTimeMarker(db ethdb.KeyValueStore, key []byte) error {
	data, err := rlp.EncodeToBytes(uint64(time.Now().Unix()))
	if err != nil {
		return err
	}
	return db.Put(key, data)
}

// ReadTimeMarker reads the timestamp stored at [key]
func ReadTimeMarker(db ethdb.KeyValueStore, key []byte) (time.Time, error) {
	data, err := db.Get(key)
	if err != nil {
		return time.Time{}, err
	}

	var lastRun uint64
	if err := rlp.DecodeBytes(data, &lastRun); err != nil {
		return time.Time{}, err
	}

	return time.Unix(int64(lastRun), 0), nil
}

// DeleteTimeMarker deletes any value stored at [key]
func DeleteTimeMarker(db ethdb.KeyValueStore, key []byte) error {
	return db.Delete(key)
}

// WriteOfflinePruning writes a marker of the last attempt to run offline pruning
// The marker is written when offline pruning completes and is deleted when the node
// is started successfully with offline pruning disabled. This ensures users must
// disable offline pruning and start their node successfully between runs of offline
// pruning.
func WriteOfflinePruning(db ethdb.KeyValueStore) error {
	return WriteTimeMarker(db, offlinePruningKey)
}

// ReadOfflinePruning reads the most recent timestamp of an attempt to run offline
// pruning if present.
func ReadOfflinePruning(db ethdb.KeyValueStore) (time.Time, error) {
	return ReadTimeMarker(db, offlinePruningKey)
}

// DeleteOfflinePruning deletes any marker of the last attempt to run offline pruning.
func DeleteOfflinePruning(db ethdb.KeyValueStore) error {
	return DeleteTimeMarker(db, offlinePruningKey)
}

// WritePopulateMissingTries writes a marker for the current attempt to populate
// missing tries.
func WritePopulateMissingTries(db ethdb.KeyValueStore) error {
	return WriteTimeMarker(db, populateMissingTriesKey)
}

// ReadPopulateMissingTries reads the most recent timestamp of an attempt to
// re-populate missing trie nodes.
func ReadPopulateMissingTries(db ethdb.KeyValueStore) (time.Time, error) {
	return ReadTimeMarker(db, populateMissingTriesKey)
}

// DeletePopulateMissingTries deletes any marker of the last attempt to
// re-populate missing trie nodes.
func DeletePopulateMissingTries(db ethdb.KeyValueStore) error {
	return DeleteTimeMarker(db, populateMissingTriesKey)
}

// WritePruningDisabled writes a marker to track whether the node has ever run
// with pruning disabled.
func WritePruningDisabled(db ethdb.KeyValueStore) error {
	return db.Put(pruningDisabledKey, nil)
}

// HasPruningDisabled returns true if there is a marker present indicating that
// the node has run with pruning disabled at some pooint.
func HasPruningDisabled(db ethdb.KeyValueStore) (bool, error) {
	return db.Has(pruningDisabledKey)
}

// DeletePruningDisabled deletes the marker indicating that the node has
// run with pruning disabled.
func DeletePruningDisabled(db ethdb.KeyValueStore) error {
	return db.Delete(pruningDisabledKey)
}

// WriteAcceptorTip writes [hash] as the last accepted block that has been fully processed.
func WriteAcceptorTip(db ethdb.KeyValueWriter, hash common.Hash) error {
	return db.Put(acceptorTipKey, hash[:])
}

// ReadAcceptorTip reads the hash of the last accepted block that was fully processed.
// If there is no value present (the index is being initialized for the first time), then the
// empty hash is returned.
func ReadAcceptorTip(db ethdb.KeyValueReader) (common.Hash, error) {
	has, err := db.Has(acceptorTipKey)
	// If the index is not present on disk, the [acceptorTipKey] index has not been initialized yet.
	if !has || err != nil {
		return common.Hash{}, err
	}
	h, err := db.Get(acceptorTipKey)
	if err != nil {
		return common.Hash{}, err
	}
	return common.BytesToHash(h), nil
}
