// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
)

var lastSyncKey = prefixdb.MakePrefix([]byte("lastSync"))

func ReadLastSync(db database.KeyValueReader) ([]byte, error) {
	return db.Get(lastSyncKey)
}

func HasLastSync(db database.KeyValueReader) (bool, error) {
	return db.Has(lastSyncKey)
}

func WriteLastSync(db database.KeyValueWriter, b []byte) error {
	return db.Put(lastSyncKey, b)
}
