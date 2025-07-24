// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"github.com/ava-labs/coreth/triedb/firewood"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
)

func NewDatabaseWithConfig(db ethdb.Database, config *triedb.Config) state.Database {
	coredb := state.NewDatabaseWithConfig(db, config)
	return wrapIfFirewood(coredb)
}

func NewDatabaseWithNodeDB(db ethdb.Database, triedb *triedb.Database) state.Database {
	coredb := state.NewDatabaseWithNodeDB(db, triedb)
	return wrapIfFirewood(coredb)
}

func wrapIfFirewood(db state.Database) state.Database {
	fw, ok := db.TrieDB().Backend().(*firewood.Database)
	if !ok {
		return db
	}
	return &firewoodAccessorDb{
		Database: db,
		fw:       fw,
	}
}
