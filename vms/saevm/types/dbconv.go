// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

// This single function is in a standalone file to reduce confusion because
// every required import has something to do with a database!

import (
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

func NewEthDB(db database.Database) ethdb.Database {
	return rawdb.NewDatabase(evmdb.New(db))
}
