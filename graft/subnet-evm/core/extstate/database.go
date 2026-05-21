// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"github.com/MetalBlockchain/libevm/core/state"
	"github.com/MetalBlockchain/libevm/ethdb"
	"github.com/MetalBlockchain/libevm/triedb"

	"github.com/MetalBlockchain/metalgo/graft/evm/firewood"
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
	fw, ok := db.TrieDB().Backend().(*firewood.TrieDB)
	if !ok {
		return db
	}
	return firewood.NewStateAccessor(db, fw)
}
