// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	// snapshotBlockHashKey tracks the block hash of the last snapshot.
	snapshotBlockHashKey = []byte("SnapshotBlockHash")
	// offlinePruningKey tracks runs of offline pruning
	offlinePruningKey = []byte("OfflinePruning")
	// populateMissingTriesKey tracks runs of trie backfills
	populateMissingTriesKey = []byte("PopulateMissingTries")
	// pruningDisabledKey tracks whether the node has ever run in archival mode
	// to ensure that a user does not accidentally corrupt an archival node.
	pruningDisabledKey = []byte("PruningDisabled")
	// acceptorTipKey tracks the tip of the last accepted block that has been fully processed.
	acceptorTipKey = []byte("AcceptorTipKey")
	// upgradeConfigPrefix prefixes upgrade bytes passed to the chain
	upgradeConfigPrefix = []byte("upgrade-config-")
)

// State sync progress keys and prefixes
var (
	// syncRootKey indicates the root of the main account trie currently being synced
	syncRootKey = []byte("sync_root")
	// syncStorageTriesPrefix is the prefix for storage tries that need to be fetched.
	// syncStorageTriesPrefix + trie root + account hash: indicates a storage trie must be fetched for the account
	syncStorageTriesPrefix = []byte("sync_storage")
	// syncSegmentsPrefix is the prefix for segments.
	// syncSegmentsPrefix + trie root + 32-byte start key: indicates the trie at root has a segment starting at the specified key
	syncSegmentsPrefix = []byte("sync_segments")
	// CodeToFetchPrefix is the prefix for code hashes that need to be fetched.
	// CodeToFetchPrefix + code hash -> empty value tracks the outstanding code hashes we need to fetch.
	CodeToFetchPrefix = []byte("CP")
	// cleanupInProgressKey tracks whether cleanup is currently in progress
	// Used to detect and recover from interrupted cleanup operations (e.g., crash during cleanup)
	cleanupInProgressKey = []byte("cleanup_in_progress")
)

// State sync progress key lengths
var (
	syncStorageTriesKeyLength = len(syncStorageTriesPrefix) + 2*common.HashLength
	syncSegmentsKeyLength     = len(syncSegmentsPrefix) + 2*common.HashLength
	codeToFetchKeyLength      = len(CodeToFetchPrefix) + common.HashLength
)

// State sync metadata
var (
	syncPerformedPrefix = []byte("sync_performed")
	// syncPerformedKeyLength is the length of the key for the sync performed metadata key,
	// and is equal to [syncPerformedPrefix] + block number as uint64.
	syncPerformedKeyLength = len(syncPerformedPrefix) + wrappers.LongLen
)

// Sync mode switching keys
var (
	// syncModeKey stores the current sync mode ("state", "block", or "hybrid")
	syncModeKey = []byte("sync_mode")
	// stateSyncLastHeightKey stores the last accepted height from state sync
	// Used to resume block sync from the correct point after state sync
	stateSyncLastHeightKey = []byte("state_sync_last_height")
	// blockSyncProgressKey stores the current block sync progress
	// Used to resume state sync or switch modes without losing progress
	blockSyncProgressKey = []byte("block_sync_progress")
	// modeSwitchHistoryPrefix prefixes mode switch event records
	// modeSwitchHistoryPrefix + timestamp -> JSON record of mode switch
	modeSwitchHistoryPrefix = []byte("mode_switch_")
)

// Missing code tracking for hybrid sync
var (
	// missingCodePrefix stores code hashes that failed to sync from network
	// missingCodePrefix + code hash + block number -> empty marker
	// Used to track which code needs to be recovered via block execution
	missingCodePrefix = []byte("missing_code_")
	// codeRecoveryProgressKey stores the current block height during code recovery
	codeRecoveryProgressKey = []byte("code_recovery_progress")
)

var (
	missingCodeKeyLength = len(missingCodePrefix) + common.HashLength + wrappers.LongLen
)

var FirewoodScheme = "firewood"

// upgradeConfigKey = upgradeConfigPrefix + hash
func upgradeConfigKey(hash common.Hash) []byte {
	return append(upgradeConfigPrefix, hash.Bytes()...)
}
