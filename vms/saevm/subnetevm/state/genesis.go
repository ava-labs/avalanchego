// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
)

var lastSyncKey = prefixdb.MakePrefix([]byte("lastSync"))

// ReadLastSync returns the RLP encoding of the last synchronously executed
// block, when one was recorded.
//
// TODO: nothing writes this key yet; transition support (materializing a
// legacy chain's tip as the last synchronous block) will reintroduce a
// writer.
func ReadLastSync(db database.KeyValueReader) ([]byte, error) {
	return db.Get(lastSyncKey)
}
