// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package manager

import (
	"sort"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/version"
)

type VersionedDatabase struct {
	Database database.Database
	Version  version.Version
}

// Close the underlying database
func (db *VersionedDatabase) Close() error {
	return db.Database.Close()
}

type innerSortDescendingVersionedDBs []*VersionedDatabase

// Less returns true if the version at index i is greater than the version at index j
// such that it will sort in descending order (newest version --> oldest version)
func (dbs innerSortDescendingVersionedDBs) Less(i, j int) bool {
	return dbs[i].Version.Compare(dbs[j].Version) > 0
}

func (dbs innerSortDescendingVersionedDBs) Len() int      { return len(dbs) }
func (dbs innerSortDescendingVersionedDBs) Swap(i, j int) { dbs[j], dbs[i] = dbs[i], dbs[j] }

func SortDescending(dbs []*VersionedDatabase) { sort.Sort(innerSortDescendingVersionedDBs(dbs)) }
