// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rawdb

import (
	ethrawdb "github.com/ava-labs/libevm/core/rawdb"
)

// Types used directly as their upstream definition.
type (
	LegacyTxLookupEntry = ethrawdb.LegacyTxLookupEntry
	OpenOptions         = ethrawdb.OpenOptions
)

// Constants used directly as their upstream definition.
const (
	PathScheme = ethrawdb.PathScheme
)

// Variables used directly as their upstream definition.
var (
	BloomBitsIndexPrefix = ethrawdb.BloomBitsIndexPrefix
	CodePrefix           = ethrawdb.CodePrefix
)

// Functions used directly as their upstream definition.
var (
	DeleteAccountSnapshot       = ethrawdb.DeleteAccountSnapshot
	DeleteAccountTrieNode       = ethrawdb.DeleteAccountTrieNode
	DeleteBlock                 = ethrawdb.DeleteBlock
	DeleteCanonicalHash         = ethrawdb.DeleteCanonicalHash
	DeleteSnapshotRoot          = ethrawdb.DeleteSnapshotRoot
	DeleteStorageSnapshot       = ethrawdb.DeleteStorageSnapshot
	DeleteStorageTrieNode       = ethrawdb.DeleteStorageTrieNode
	DeleteTrieJournal           = ethrawdb.DeleteTrieJournal
	DeleteTrieNode              = ethrawdb.DeleteTrieNode
	ExistsAccountTrieNode       = ethrawdb.ExistsAccountTrieNode
	FindCommonAncestor          = ethrawdb.FindCommonAncestor
	HasBody                     = ethrawdb.HasBody
	HasCode                     = ethrawdb.HasCode
	HasHeader                   = ethrawdb.HasHeader
	HashScheme                  = ethrawdb.HashScheme
	HasLegacyTrieNode           = ethrawdb.HasLegacyTrieNode
	HasReceipts                 = ethrawdb.HasReceipts
	IsCodeKey                   = ethrawdb.IsCodeKey
	IterateStorageSnapshots     = ethrawdb.IterateStorageSnapshots
	NewDatabase                 = ethrawdb.NewDatabase
	NewDatabaseWithFreezer      = ethrawdb.NewDatabaseWithFreezer
	NewKeyLengthIterator        = ethrawdb.NewKeyLengthIterator
	NewLevelDBDatabase          = ethrawdb.NewLevelDBDatabase
	NewMemoryDatabase           = ethrawdb.NewMemoryDatabase
	NewStateFreezer             = ethrawdb.NewStateFreezer
	NewTable                    = ethrawdb.NewTable
	Open                        = ethrawdb.Open
	ParseStateScheme            = ethrawdb.ParseStateScheme
	PopUncleanShutdownMarker    = ethrawdb.PopUncleanShutdownMarker
	PushUncleanShutdownMarker   = ethrawdb.PushUncleanShutdownMarker
	ReadAccountSnapshot         = ethrawdb.ReadAccountSnapshot
	ReadAccountTrieNode         = ethrawdb.ReadAccountTrieNode
	ReadAllHashes               = ethrawdb.ReadAllHashes
	ReadBlock                   = ethrawdb.ReadBlock
	ReadBloomBits               = ethrawdb.ReadBloomBits
	ReadBody                    = ethrawdb.ReadBody
	ReadCanonicalHash           = ethrawdb.ReadCanonicalHash
	ReadChainConfig             = ethrawdb.ReadChainConfig
	ReadCode                    = ethrawdb.ReadCode
	ReadDatabaseVersion         = ethrawdb.ReadDatabaseVersion
	ReadHeadBlock               = ethrawdb.ReadHeadBlock
	ReadHeadBlockHash           = ethrawdb.ReadHeadBlockHash
	ReadHeader                  = ethrawdb.ReadHeader
	ReadHeaderNumber            = ethrawdb.ReadHeaderNumber
	ReadHeadFastBlockHash       = ethrawdb.ReadHeadFastBlockHash
	ReadHeadHeaderHash          = ethrawdb.ReadHeadHeaderHash
	ReadLastPivotNumber         = ethrawdb.ReadLastPivotNumber
	ReadLegacyTrieNode          = ethrawdb.ReadLegacyTrieNode
	ReadLogs                    = ethrawdb.ReadLogs
	ReadPersistentStateID       = ethrawdb.ReadPersistentStateID
	ReadPreimage                = ethrawdb.ReadPreimage
	ReadRawReceipts             = ethrawdb.ReadRawReceipts
	ReadReceipts                = ethrawdb.ReadReceipts
	ReadSkeletonSyncStatus      = ethrawdb.ReadSkeletonSyncStatus
	ReadSnapshotDisabled        = ethrawdb.ReadSnapshotDisabled
	ReadSnapshotGenerator       = ethrawdb.ReadSnapshotGenerator
	ReadSnapshotJournal         = ethrawdb.ReadSnapshotJournal
	ReadSnapshotRecoveryNumber  = ethrawdb.ReadSnapshotRecoveryNumber
	ReadSnapshotRoot            = ethrawdb.ReadSnapshotRoot
	ReadSnapshotSyncStatus      = ethrawdb.ReadSnapshotSyncStatus
	ReadSnapSyncStatusFlag      = ethrawdb.ReadSnapSyncStatusFlag
	ReadStateID                 = ethrawdb.ReadStateID
	ReadStorageSnapshot         = ethrawdb.ReadStorageSnapshot
	ReadStorageTrieNode         = ethrawdb.ReadStorageTrieNode
	ReadTransaction             = ethrawdb.ReadTransaction
	ReadTrieJournal             = ethrawdb.ReadTrieJournal
	ReadTxIndexTail             = ethrawdb.ReadTxIndexTail
	ReadTxLookupEntry           = ethrawdb.ReadTxLookupEntry
	SnapshotAccountPrefix       = ethrawdb.SnapshotAccountPrefix
	SnapshotStoragePrefix       = ethrawdb.SnapshotStoragePrefix
	UnindexTransactions         = ethrawdb.UnindexTransactions
	UpdateUncleanShutdownMarker = ethrawdb.UpdateUncleanShutdownMarker
	WriteAccountSnapshot        = ethrawdb.WriteAccountSnapshot
	WriteAccountTrieNode        = ethrawdb.WriteAccountTrieNode
	WriteBlock                  = ethrawdb.WriteBlock
	WriteBloomBits              = ethrawdb.WriteBloomBits
	WriteBody                   = ethrawdb.WriteBody
	WriteCanonicalHash          = ethrawdb.WriteCanonicalHash
	WriteChainConfig            = ethrawdb.WriteChainConfig
	WriteCode                   = ethrawdb.WriteCode
	WriteDatabaseVersion        = ethrawdb.WriteDatabaseVersion
	WriteHeadBlockHash          = ethrawdb.WriteHeadBlockHash
	WriteHeader                 = ethrawdb.WriteHeader
	WriteHeadHeaderHash         = ethrawdb.WriteHeadHeaderHash
	WriteLegacyTrieNode         = ethrawdb.WriteLegacyTrieNode
	WritePersistentStateID      = ethrawdb.WritePersistentStateID
	WritePreimages              = ethrawdb.WritePreimages
	WriteReceipts               = ethrawdb.WriteReceipts
	WriteSnapshotGenerator      = ethrawdb.WriteSnapshotGenerator
	WriteSnapshotRoot           = ethrawdb.WriteSnapshotRoot
	WriteSnapSyncStatusFlag     = ethrawdb.WriteSnapSyncStatusFlag
	WriteStateID                = ethrawdb.WriteStateID
	WriteStorageSnapshot        = ethrawdb.WriteStorageSnapshot
	WriteStorageTrieNode        = ethrawdb.WriteStorageTrieNode
	WriteTrieJournal            = ethrawdb.WriteTrieJournal
	WriteTrieNode               = ethrawdb.WriteTrieNode
	WriteTxIndexTail            = ethrawdb.WriteTxIndexTail
	WriteTxLookupEntriesByBlock = ethrawdb.WriteTxLookupEntriesByBlock
)
