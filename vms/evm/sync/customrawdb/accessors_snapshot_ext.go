// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/log"
)

// WriteSnapshotBlockHash stores the root of the block whose state is contained in
// the persisted snapshot.
func WriteSnapshotBlockHash(db ethdb.KeyValueWriter, blockHash common.Hash) {
	if err := db.Put(snapshotBlockHashKey, blockHash[:]); err != nil {
		log.Crit("Failed to store snapshot block hash", "err", err)
	}
}
