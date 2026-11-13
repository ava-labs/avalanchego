// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package types

// These functions are in a standalone file to reduce confusion because
// every required import has something to do with a database!

import (
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"

	evmdb "github.com/ava-labs/avalanchego/vms/evm/imp/database"
)

func NewEthDB(db database.Database) ethdb.Database {
	return rawdb.NewDatabase(evmdb.New(db))
}

var ethDBPrefix = []byte("ethdb")

// NewChainEthDB returns the [ethdb.Database] carved out of a chain's
// avalanchego-provided database under the conventional "ethdb" prefix.
//
// The use of [prefixdb.NewNested] rather than [prefixdb.New] is part of the
// on-disk format and MUST NOT change.
func NewChainEthDB(db database.Database) ethdb.Database {
	return NewEthDB(prefixdb.NewNested(ethDBPrefix, db))
}
