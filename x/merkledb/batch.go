// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"github.com/ava-labs/avalanchego/database"
)

var _ database.Batch = &batch{}

// batch is a write-only database that commits changes to its host database
// when Write is called.
type batch struct {
	database.BatchOps

	db *Database
}

// apply all operations in order to the database and write the result to disk
func (b *batch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	return b.db.commitBatch(b.Ops)
}

func (b *batch) Inner() database.Batch {
	return b
}
