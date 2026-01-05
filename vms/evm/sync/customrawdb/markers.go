// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/database"
)

var (
	// errInvalidData indicates the stored value exists but is malformed or undecodable.
	errInvalidData                  = errors.New("invalid data")
	errFailedToGetUpgradeConfig     = errors.New("failed to get upgrade config")
	errFailedToMarshalUpgradeConfig = errors.New("failed to marshal upgrade config")

	upgradeConfigPrefix = []byte("upgrade-config-")
	// offlinePruningKey tracks runs of offline pruning.
	offlinePruningKey = []byte("OfflinePruning")
	// populateMissingTriesKey tracks runs of trie backfills.
	populateMissingTriesKey = []byte("PopulateMissingTries")
	// pruningDisabledKey tracks whether the node has ever run in archival mode
	// to ensure that a user does not accidentally corrupt an archival node.
	pruningDisabledKey = []byte("PruningDisabled")
	// acceptorTipKey tracks the tip of the last accepted block that has been fully processed.
	acceptorTipKey = []byte("AcceptorTipKey")
	// snapshotBlockHashKey tracks the block hash of the last snapshot.
	snapshotBlockHashKey = []byte("SnapshotBlockHash")
)

// WriteOfflinePruning writes a time marker of the last attempt to run offline pruning.
// The marker is written when offline pruning completes and is deleted when the node
// is started successfully with offline pruning disabled. This ensures users must
// disable offline pruning and start their node successfully between runs of offline
// pruning.
func WriteOfflinePruning(db ethdb.KeyValueWriter, ts time.Time) error {
	return writeTimeMarker(db, offlinePruningKey, ts)
}

// ReadOfflinePruning reads the most recent timestamp of an attempt to run offline
// pruning if present.
func ReadOfflinePruning(db ethdb.KeyValueReader) (time.Time, error) {
	return readTimeMarker(db, offlinePruningKey)
}

// DeleteOfflinePruning deletes any marker of the last attempt to run offline pruning.
func DeleteOfflinePruning(db ethdb.KeyValueWriter) error {
	return db.Delete(offlinePruningKey)
}

// WritePopulateMissingTries writes a marker for the current attempt to populate
// missing tries.
func WritePopulateMissingTries(db ethdb.KeyValueWriter, ts time.Time) error {
	return writeTimeMarker(db, populateMissingTriesKey, ts)
}

// ReadPopulateMissingTries reads the most recent timestamp of an attempt to
// re-populate missing trie nodes.
func ReadPopulateMissingTries(db ethdb.KeyValueReader) (time.Time, error) {
	return readTimeMarker(db, populateMissingTriesKey)
}

// DeletePopulateMissingTries deletes any marker of the last attempt to
// re-populate missing trie nodes.
func DeletePopulateMissingTries(db ethdb.KeyValueWriter) error {
	return db.Delete(populateMissingTriesKey)
}

// WritePruningDisabled writes a marker to track whether the node has ever run
// with pruning disabled.
func WritePruningDisabled(db ethdb.KeyValueWriter) error {
	return db.Put(pruningDisabledKey, nil)
}

// HasPruningDisabled returns true if there is a marker present indicating that
// the node has run with pruning disabled at some point.
func HasPruningDisabled(db ethdb.KeyValueReader) (bool, error) {
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
	ok, err := db.Has(acceptorTipKey)
	if err != nil {
		return common.Hash{}, err
	}
	if !ok {
		return common.Hash{}, database.ErrNotFound
	}
	h, err := db.Get(acceptorTipKey)
	if err != nil {
		return common.Hash{}, err
	}
	if len(h) != common.HashLength {
		return common.Hash{}, fmt.Errorf("%w: length %d", errInvalidData, len(h))
	}
	return common.BytesToHash(h), nil
}

// ReadChainConfig retrieves the consensus settings based on the given genesis hash.
// The provided `upgradeConfig` (any JSON-unmarshalable type) will be populated if present on disk.
func ReadChainConfig[T any](db ethdb.KeyValueReader, hash common.Hash, upgradeConfig *T) (*params.ChainConfig, error) {
	config := rawdb.ReadChainConfig(db, hash)
	if config == nil {
		return nil, database.ErrNotFound
	}

	upgrade, err := db.Get(upgradeConfigKey(hash))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errFailedToGetUpgradeConfig, err)
	}

	if len(upgrade) == 0 {
		return config, nil
	}

	if err := json.Unmarshal(upgrade, upgradeConfig); err != nil {
		return nil, errInvalidData
	}

	return config, nil
}

// WriteChainConfig writes the chain config settings to the database.
// The provided `upgradeConfig` (any JSON-marshalable type) will be stored alongside the chain config.
func WriteChainConfig[T any](db ethdb.KeyValueWriter, hash common.Hash, config *params.ChainConfig, upgradeConfig T) error {
	rawdb.WriteChainConfig(db, hash, config)
	if config == nil {
		return nil
	}

	data, err := json.Marshal(upgradeConfig)
	if err != nil {
		return fmt.Errorf("%w: %w", errFailedToMarshalUpgradeConfig, err)
	}
	return db.Put(upgradeConfigKey(hash), data)
}

// NewAccountSnapshotsIterator returns an iterator for walking all of the accounts in the snapshot
func NewAccountSnapshotsIterator(db ethdb.Iteratee) ethdb.Iterator {
	it := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
	keyLen := len(rawdb.SnapshotAccountPrefix) + common.HashLength
	return rawdb.NewKeyLengthIterator(it, keyLen)
}

// ReadSnapshotBlockHash retrieves the hash of the block whose state is contained in
// the persisted snapshot.
func ReadSnapshotBlockHash(db ethdb.KeyValueReader) (common.Hash, error) {
	ok, err := db.Has(snapshotBlockHashKey)
	if err != nil {
		return common.Hash{}, err
	}
	if !ok {
		return common.Hash{}, database.ErrNotFound
	}

	data, err := db.Get(snapshotBlockHashKey)
	if err != nil {
		return common.Hash{}, err
	}
	if len(data) != common.HashLength {
		return common.Hash{}, fmt.Errorf("%w: length %d", errInvalidData, len(data))
	}
	return common.BytesToHash(data), nil
}

// WriteSnapshotBlockHash stores the root of the block whose state is contained in
// the persisted snapshot.
func WriteSnapshotBlockHash(db ethdb.KeyValueWriter, blockHash common.Hash) error {
	return db.Put(snapshotBlockHashKey, blockHash[:])
}

// DeleteSnapshotBlockHash deletes the hash of the block whose state is contained in
// the persisted snapshot. Since snapshots are not immutable, this method can
// be used during updates, so a crash or failure will mark the entire snapshot
// invalid.
func DeleteSnapshotBlockHash(db ethdb.KeyValueWriter) error {
	return db.Delete(snapshotBlockHashKey)
}

func writeTimeMarker(db ethdb.KeyValueWriter, key []byte, ts time.Time) error {
	data, err := rlp.EncodeToBytes(uint64(ts.Unix()))
	if err != nil {
		return err
	}
	return db.Put(key, data)
}

func readTimeMarker(db ethdb.KeyValueReader, key []byte) (time.Time, error) {
	// Check existence first to map missing marker to a stable sentinel error.
	ok, err := db.Has(key)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Time{}, database.ErrNotFound
	}

	data, err := db.Get(key)
	if err != nil {
		return time.Time{}, err
	}

	var unix uint64
	if err := rlp.DecodeBytes(data, &unix); err != nil {
		return time.Time{}, fmt.Errorf("%w: %w", errInvalidData, err)
	}

	return time.Unix(int64(unix), 0), nil
}

func upgradeConfigKey(hash common.Hash) []byte {
	return append(upgradeConfigPrefix, hash.Bytes()...)
}
