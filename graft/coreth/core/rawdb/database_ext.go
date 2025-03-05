// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rawdb

import (
	"bytes"

	"github.com/ava-labs/libevm/common"
	ethrawdb "github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
)

// InspectDatabase traverses the entire database and checks the size
// of all different categories of data.
func InspectDatabase(db ethdb.Database, keyPrefix, keyStart []byte) error {
	var (
		codeToFetch   ethrawdb.DatabaseStat
		syncPerformed ethrawdb.DatabaseStat
		syncProgress  ethrawdb.DatabaseStat
		syncSegments  ethrawdb.DatabaseStat
	)

	options := []ethrawdb.InspectDatabaseOption{
		ethrawdb.WithDatabaseMetadataKeys(func(key []byte) bool {
			return bytes.Equal(key, snapshotBlockHashKey) ||
				bytes.Equal(key, syncRootKey)
		}),
		ethrawdb.WithDatabaseStatRecorder(func(key []byte, size common.StorageSize) bool {
			switch {
			case bytes.HasPrefix(key, syncSegmentsPrefix) && len(key) == syncSegmentsKeyLength:
				syncSegments.Add(size)
				return true
			case bytes.HasPrefix(key, syncStorageTriesPrefix) && len(key) == syncStorageTriesKeyLength:
				syncProgress.Add(size)
				return true
			case bytes.HasPrefix(key, CodeToFetchPrefix) && len(key) == codeToFetchKeyLength:
				codeToFetch.Add(size)
				return true
			case bytes.HasPrefix(key, syncPerformedPrefix) && len(key) == syncPerformedKeyLength:
				syncPerformed.Add(size)
				return true
			default:
				return false
			}
		}),
		ethrawdb.WithDatabaseStatsTransformer(func(rows [][]string) [][]string {
			newRows := make([][]string, 0, len(rows))
			for _, row := range rows {
				database := row[0]
				category := row[1]
				switch {
				case database == "Key-Value store" && category == "Difficulties",
					database == "Key-Value store" && category == "Beacon sync headers",
					database == "Ancient store (Chain)":
					// Discard rows specific to libevm (geth) but irrelevant to coreth.
					continue
				}
				newRows = append(newRows, row)
			}

			return append(
				newRows,
				[]string{"State sync", "Trie segments", syncSegments.Size(), syncSegments.Count()},
				[]string{"State sync", "Storage tries to fetch", syncProgress.Size(), syncProgress.Count()},
				[]string{"State sync", "Code to fetch", codeToFetch.Size(), codeToFetch.Count()},
				[]string{"State sync", "Block numbers synced to", syncPerformed.Size(), syncPerformed.Count()},
			)
		}),
	}

	return ethrawdb.InspectDatabase(db, keyPrefix, keyStart, options...)
}
