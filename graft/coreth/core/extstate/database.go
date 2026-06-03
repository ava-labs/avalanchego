// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package extstate

import (
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/graft/evm/firewood"
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
	switch fw := db.TrieDB().Backend().(type) {
	case *firewood.TrieDB:
		return firewood.NewStateAccessor(db, fw)
	case *firewood.ReconstructedTrieDB:
		return firewood.NewReconstructedStateAccessor(db, fw)
	default:
		return db
	}
}
