// (c) 2019-2025, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2017 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package state

import (
	ethstate "github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/subnet-evm/triedb/firewood"
)

type (
	Database = ethstate.Database
	Trie     = ethstate.Trie
)

func NewDatabase(db ethdb.Database) Database {
	return ethstate.NewDatabase(db)
}

func NewDatabaseWithConfig(db ethdb.Database, config *triedb.Config) Database {
	coredb := ethstate.NewDatabaseWithConfig(db, config)
	return wrapIfFirewood(coredb)
}

func NewDatabaseWithNodeDB(db ethdb.Database, triedb *triedb.Database) Database {
	coredb := ethstate.NewDatabaseWithNodeDB(db, triedb)
	return wrapIfFirewood(coredb)
}
func wrapIfFirewood(db Database) Database {
	fw, ok := db.TrieDB().Backend().(*firewood.Database)
	if !ok {
		return db
	}
	return &firewoodAccessorDb{
		Database: db,
		fw:       fw,
	}
}
