// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package manager

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/version"
)

type VersionedDatabase struct {
	Database database.Database
	Version  *version.Semantic
}

// Close the underlying database
func (db *VersionedDatabase) Close() error {
	return db.Database.Close()
}

func (db *VersionedDatabase) Less(other *VersionedDatabase) bool {
	// Note this sorts in descending order (newest version --> oldest version)
	return db.Version.Compare(other.Version) > 0
}
