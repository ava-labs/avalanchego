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

// Package rawdb contains a collection of low level database accessors.
package rawdb

import (
	"bytes"
	"encoding/binary"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/metrics"
)

// The fields below define the low level database schema prefixing.
var (
	// databaseVersionKey tracks the current database version.
	databaseVersionKey = []byte("DatabaseVersion")

	// headHeaderKey tracks the latest known header's hash.
	headHeaderKey = []byte("LastHeader")

	// headBlockKey tracks the latest known full block's hash.
	headBlockKey = []byte("LastBlock")

	// persistentStateIDKey tracks the id of latest stored state(for path-based only).
	persistentStateIDKey = []byte("LastStateID")

	// snapshotRootKey tracks the hash of the last snapshot.
	snapshotRootKey = []byte("SnapshotRoot")

	// snapshotBlockHashKey tracks the block hash of the last snapshot.
	snapshotBlockHashKey = []byte("SnapshotBlockHash")

	// snapshotGeneratorKey tracks the snapshot generation marker across restarts.
	snapshotGeneratorKey = []byte("SnapshotGenerator")

	// trieJournalKey tracks the in-memory trie node layers across restarts.
	trieJournalKey = []byte("TrieJournal")

	// txIndexTailKey tracks the oldest block whose transactions have been indexed.
	txIndexTailKey = []byte("TransactionIndexTail")

	// uncleanShutdownKey tracks the list of local crashes
	uncleanShutdownKey = []byte("unclean-shutdown") // config prefix for the db

	// offlinePruningKey tracks runs of offline pruning
	offlinePruningKey = []byte("OfflinePruning")

	// populateMissingTriesKey tracks runs of trie backfills
	populateMissingTriesKey = []byte("PopulateMissingTries")

	// pruningDisabledKey tracks whether the node has ever run in archival mode
	// to ensure that a user does not accidentally corrupt an archival node.
	pruningDisabledKey = []byte("PruningDisabled")

	// acceptorTipKey tracks the tip of the last accepted block that has been fully processed.
	acceptorTipKey = []byte("AcceptorTipKey")

	// Data item prefixes (use single byte to avoid mixing data types, avoid `i`, used for indexes).
	headerPrefix       = []byte("h") // headerPrefix + num (uint64 big endian) + hash -> header
	headerHashSuffix   = []byte("n") // headerPrefix + num (uint64 big endian) + headerHashSuffix -> hash
	headerNumberPrefix = []byte("H") // headerNumberPrefix + hash -> num (uint64 big endian)

	blockBodyPrefix     = []byte("b") // blockBodyPrefix + num (uint64 big endian) + hash -> block body
	blockReceiptsPrefix = []byte("r") // blockReceiptsPrefix + num (uint64 big endian) + hash -> block receipts

	txLookupPrefix        = []byte("l") // txLookupPrefix + hash -> transaction/receipt lookup metadata
	bloomBitsPrefix       = []byte("B") // bloomBitsPrefix + bit (uint16 big endian) + section (uint64 big endian) + hash -> bloom bits
	SnapshotAccountPrefix = []byte("a") // SnapshotAccountPrefix + account hash -> account trie value
	SnapshotStoragePrefix = []byte("o") // SnapshotStoragePrefix + account hash + storage hash -> storage trie value
	CodePrefix            = []byte("c") // CodePrefix + code hash -> account code

	// Path-based storage scheme of merkle patricia trie.
	trieNodeAccountPrefix = []byte("A") // trieNodeAccountPrefix + hexPath -> trie node
	trieNodeStoragePrefix = []byte("O") // trieNodeStoragePrefix + accountHash + hexPath -> trie node
	stateIDPrefix         = []byte("L") // stateIDPrefix + state root -> state id

	PreimagePrefix = []byte("secure-key-")      // PreimagePrefix + hash -> preimage
	configPrefix   = []byte("ethereum-config-") // config prefix for the db

	// BloomBitsIndexPrefix is the data table of a chain indexer to track its progress
	BloomBitsIndexPrefix = []byte("iB")

	preimageCounter    = metrics.NewRegisteredCounter("db/preimage/total", nil)
	preimageHitCounter = metrics.NewRegisteredCounter("db/preimage/hits", nil)

	// State sync progress keys and prefixes
	syncRootKey            = []byte("sync_root")     // indicates the root of the main account trie currently being synced
	syncStorageTriesPrefix = []byte("sync_storage")  // syncStorageTriesPrefix + trie root + account hash: indicates a storage trie must be fetched for the account
	syncSegmentsPrefix     = []byte("sync_segments") // syncSegmentsPrefix + trie root + 32-byte start key: indicates the trie at root has a segment starting at the specified key
	CodeToFetchPrefix      = []byte("CP")            // CodeToFetchPrefix + code hash -> empty value tracks the outstanding code hashes we need to fetch.

	// State sync progress key lengths
	syncStorageTriesKeyLength = len(syncStorageTriesPrefix) + 2*common.HashLength
	syncSegmentsKeyLength     = len(syncSegmentsPrefix) + 2*common.HashLength
	codeToFetchKeyLength      = len(CodeToFetchPrefix) + common.HashLength

	// State sync metadata
	syncPerformedPrefix    = []byte("sync_performed")
	syncPerformedKeyLength = len(syncPerformedPrefix) + wrappers.LongLen // prefix + block number as uint64
)

// LegacyTxLookupEntry is the legacy TxLookupEntry definition with some unnecessary
// fields.
type LegacyTxLookupEntry struct {
	BlockHash  common.Hash
	BlockIndex uint64
	Index      uint64
}

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

// headerKeyPrefix = headerPrefix + num (uint64 big endian)
func headerKeyPrefix(number uint64) []byte {
	return append(headerPrefix, encodeBlockNumber(number)...)
}

// headerKey = headerPrefix + num (uint64 big endian) + hash
func headerKey(number uint64, hash common.Hash) []byte {
	return append(append(headerPrefix, encodeBlockNumber(number)...), hash.Bytes()...)
}

// headerHashKey = headerPrefix + num (uint64 big endian) + headerHashSuffix
func headerHashKey(number uint64) []byte {
	return append(append(headerPrefix, encodeBlockNumber(number)...), headerHashSuffix...)
}

// headerNumberKey = headerNumberPrefix + hash
func headerNumberKey(hash common.Hash) []byte {
	return append(headerNumberPrefix, hash.Bytes()...)
}

// blockBodyKey = blockBodyPrefix + num (uint64 big endian) + hash
func blockBodyKey(number uint64, hash common.Hash) []byte {
	return append(append(blockBodyPrefix, encodeBlockNumber(number)...), hash.Bytes()...)
}

// blockReceiptsKey = blockReceiptsPrefix + num (uint64 big endian) + hash
func blockReceiptsKey(number uint64, hash common.Hash) []byte {
	return append(append(blockReceiptsPrefix, encodeBlockNumber(number)...), hash.Bytes()...)
}

// txLookupKey = txLookupPrefix + hash
func txLookupKey(hash common.Hash) []byte {
	return append(txLookupPrefix, hash.Bytes()...)
}

// accountSnapshotKey = SnapshotAccountPrefix + hash
func accountSnapshotKey(hash common.Hash) []byte {
	return append(SnapshotAccountPrefix, hash.Bytes()...)
}

// storageSnapshotKey = SnapshotStoragePrefix + account hash + storage hash
func storageSnapshotKey(accountHash, storageHash common.Hash) []byte {
	buf := make([]byte, len(SnapshotStoragePrefix)+common.HashLength+common.HashLength)
	n := copy(buf, SnapshotStoragePrefix)
	n += copy(buf[n:], accountHash.Bytes())
	copy(buf[n:], storageHash.Bytes())
	return buf
}

// storageSnapshotsKey = SnapshotStoragePrefix + account hash + storage hash
func storageSnapshotsKey(accountHash common.Hash) []byte {
	return append(SnapshotStoragePrefix, accountHash.Bytes()...)
}

// bloomBitsKey = bloomBitsPrefix + bit (uint16 big endian) + section (uint64 big endian) + hash
func bloomBitsKey(bit uint, section uint64, hash common.Hash) []byte {
	key := append(append(bloomBitsPrefix, make([]byte, 10)...), hash.Bytes()...)

	binary.BigEndian.PutUint16(key[1:], uint16(bit))
	binary.BigEndian.PutUint64(key[3:], section)

	return key
}

// preimageKey = preimagePrefix + hash
func preimageKey(hash common.Hash) []byte {
	return append(PreimagePrefix, hash.Bytes()...)
}

// codeKey = CodePrefix + hash
func codeKey(hash common.Hash) []byte {
	return append(CodePrefix, hash.Bytes()...)
}

// IsCodeKey reports whether the given byte slice is the key of contract code,
// if so return the raw code hash as well.
func IsCodeKey(key []byte) (bool, []byte) {
	if bytes.HasPrefix(key, CodePrefix) && len(key) == common.HashLength+len(CodePrefix) {
		return true, key[len(CodePrefix):]
	}
	return false, nil
}

// configKey = configPrefix + hash
func configKey(hash common.Hash) []byte {
	return append(configPrefix, hash.Bytes()...)
}

// stateIDKey = stateIDPrefix + root (32 bytes)
func stateIDKey(root common.Hash) []byte {
	return append(stateIDPrefix, root.Bytes()...)
}

// accountTrieNodeKey = trieNodeAccountPrefix + nodePath.
func accountTrieNodeKey(path []byte) []byte {
	return append(trieNodeAccountPrefix, path...)
}

// storageTrieNodeKey = trieNodeStoragePrefix + accountHash + nodePath.
func storageTrieNodeKey(accountHash common.Hash, path []byte) []byte {
	buf := make([]byte, len(trieNodeStoragePrefix)+common.HashLength+len(path))
	n := copy(buf, trieNodeStoragePrefix)
	n += copy(buf[n:], accountHash.Bytes())
	copy(buf[n:], path)
	return buf
}

// IsLegacyTrieNode reports whether a provided database entry is a legacy trie
// node. The characteristics of legacy trie node are:
// - the key length is 32 bytes
// - the key is the hash of val
func IsLegacyTrieNode(key []byte, val []byte) bool {
	if len(key) != common.HashLength {
		return false
	}
	return bytes.Equal(key, crypto.Keccak256(val))
}

// ResolveAccountTrieNodeKey reports whether a provided database entry is an
// account trie node in path-based state scheme, and returns the resolved
// node path if so.
func ResolveAccountTrieNodeKey(key []byte) (bool, []byte) {
	if !bytes.HasPrefix(key, trieNodeAccountPrefix) {
		return false, nil
	}
	// The remaining key should only consist a hex node path
	// whose length is in the range 0 to 64 (64 is excluded
	// since leaves are always wrapped with shortNode).
	if len(key) >= len(trieNodeAccountPrefix)+common.HashLength*2 {
		return false, nil
	}
	return true, key[len(trieNodeAccountPrefix):]
}

// IsAccountTrieNode reports whether a provided database entry is an account
// trie node in path-based state scheme.
func IsAccountTrieNode(key []byte) bool {
	ok, _ := ResolveAccountTrieNodeKey(key)
	return ok
}

// ResolveStorageTrieNode reports whether a provided database entry is a storage
// trie node in path-based state scheme, and returns the resolved account hash
// and node path if so.
func ResolveStorageTrieNode(key []byte) (bool, common.Hash, []byte) {
	if !bytes.HasPrefix(key, trieNodeStoragePrefix) {
		return false, common.Hash{}, nil
	}
	// The remaining key consists of 2 parts:
	// - 32 bytes account hash
	// - hex node path whose length is in the range 0 to 64
	if len(key) < len(trieNodeStoragePrefix)+common.HashLength {
		return false, common.Hash{}, nil
	}
	if len(key) >= len(trieNodeStoragePrefix)+common.HashLength+common.HashLength*2 {
		return false, common.Hash{}, nil
	}
	accountHash := common.BytesToHash(key[len(trieNodeStoragePrefix) : len(trieNodeStoragePrefix)+common.HashLength])
	return true, accountHash, key[len(trieNodeStoragePrefix)+common.HashLength:]
}

// IsStorageTrieNode reports whether a provided database entry is a storage
// trie node in path-based state scheme.
func IsStorageTrieNode(key []byte) bool {
	ok, _, _ := ResolveStorageTrieNode(key)
	return ok
}
