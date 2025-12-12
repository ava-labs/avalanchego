// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"

	ethrawdb "github.com/ava-labs/libevm/core/rawdb"
)

// writeCurrentTimeMarker writes a marker of the current time in the db at `key`.
func writeCurrentTimeMarker(db ethdb.KeyValueStore, key []byte) error {
	data, err := rlp.EncodeToBytes(uint64(time.Now().Unix()))
	if err != nil {
		return err
	}
	return db.Put(key, data)
}

// readTimeMarker reads the timestamp stored at `key`
func readTimeMarker(db ethdb.KeyValueStore, key []byte) (time.Time, error) {
	data, err := db.Get(key)
	if err != nil {
		return time.Time{}, err
	}

	var unix uint64
	if err := rlp.DecodeBytes(data, &unix); err != nil {
		return time.Time{}, err
	}

	return time.Unix(int64(unix), 0), nil
}

// WriteOfflinePruning writes a time marker of the last attempt to run offline pruning.
// The marker is written when offline pruning completes and is deleted when the node
// is started successfully with offline pruning disabled. This ensures users must
// disable offline pruning and start their node successfully between runs of offline
// pruning.
func WriteOfflinePruning(db ethdb.KeyValueStore) error {
	return writeCurrentTimeMarker(db, offlinePruningKey)
}

// ReadOfflinePruning reads the most recent timestamp of an attempt to run offline
// pruning if present.
func ReadOfflinePruning(db ethdb.KeyValueStore) (time.Time, error) {
	return readTimeMarker(db, offlinePruningKey)
}

// DeleteOfflinePruning deletes any marker of the last attempt to run offline pruning.
func DeleteOfflinePruning(db ethdb.KeyValueStore) error {
	return db.Delete(offlinePruningKey)
}

// WritePopulateMissingTries writes a marker for the current attempt to populate
// missing tries.
func WritePopulateMissingTries(db ethdb.KeyValueStore) error {
	return writeCurrentTimeMarker(db, populateMissingTriesKey)
}

// ReadPopulateMissingTries reads the most recent timestamp of an attempt to
// re-populate missing trie nodes.
func ReadPopulateMissingTries(db ethdb.KeyValueStore) (time.Time, error) {
	return readTimeMarker(db, populateMissingTriesKey)
}

// DeletePopulateMissingTries deletes any marker of the last attempt to
// re-populate missing trie nodes.
func DeletePopulateMissingTries(db ethdb.KeyValueStore) error {
	return db.Delete(populateMissingTriesKey)
}

// WritePruningDisabled writes a marker to track whether the node has ever run
// with pruning disabled.
func WritePruningDisabled(db ethdb.KeyValueStore) error {
	return db.Put(pruningDisabledKey, nil)
}

// HasPruningDisabled returns true if there is a marker present indicating that
// the node has run with pruning disabled at some point.
func HasPruningDisabled(db ethdb.KeyValueStore) (bool, error) {
	return db.Has(pruningDisabledKey)
}

// WriteAcceptorTip writes `hash` as the last accepted block that has been fully processed.
func WriteAcceptorTip(db ethdb.KeyValueWriter, hash common.Hash) error {
	return db.Put(acceptorTipKey, hash[:])
}

// ReadAcceptorTip reads the hash of the last accepted block that was fully processed.
// If there is no value present (the index is being initialized for the first time), then the
// empty hash is returned.
func ReadAcceptorTip(db ethdb.KeyValueReader) (common.Hash, error) {
	has, err := db.Has(acceptorTipKey)
	if err != nil {
		return common.Hash{}, err
	}
	if !has {
		// If the index is not present on disk, the [acceptorTipKey] index has not been initialized yet.
		return common.Hash{}, nil
	}
	h, err := db.Get(acceptorTipKey)
	if err != nil {
		return common.Hash{}, err
	}
	if len(h) != common.HashLength {
		return common.Hash{}, fmt.Errorf("value has incorrect length %d", len(h))
	}
	return common.BytesToHash(h), nil
}

// ReadChainConfig retrieves the consensus settings based on the given genesis hash.
func ReadChainConfig(db ethdb.KeyValueReader, hash common.Hash) *params.ChainConfig {
	config := ethrawdb.ReadChainConfig(db, hash)

	upgrade, _ := db.Get(upgradeConfigKey(hash))
	if len(upgrade) == 0 {
		return config
	}

	extra := params.GetExtra(config)
	if err := json.Unmarshal(upgrade, &extra.UpgradeConfig); err != nil {
		log.Error("Invalid upgrade config JSON", "err", err)
		return nil
	}

	return config
}

// WriteChainConfig writes the chain config settings to the database.
func WriteChainConfig(db ethdb.KeyValueWriter, hash common.Hash, config *params.ChainConfig) {
	ethrawdb.WriteChainConfig(db, hash, config)
	if config == nil {
		return
	}

	extra := params.GetExtra(config)
	data, err := json.Marshal(extra.UpgradeConfig)
	if err != nil {
		log.Crit("Failed to JSON encode upgrade config", "err", err)
	}
	if err := db.Put(upgradeConfigKey(hash), data); err != nil {
		log.Crit("Failed to store upgrade config", "err", err)
	}
}
