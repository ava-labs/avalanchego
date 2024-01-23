// (c) 2020-2021, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2019 The go-ethereum Authors
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

package trie

import (
	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/trie/triedb/hashdb"
	"github.com/ethereum/go-ethereum/ethdb"
)

// newTestDatabase initializes the trie database with specified scheme.
func newTestDatabase(diskdb ethdb.Database, scheme string) *Database {
	db := prepare(diskdb, nil)
	if scheme == rawdb.HashScheme {
		db.backend = hashdb.New(diskdb, db.cleans, mptResolver{})
	}
	//} else {
	//	db.backend = snap.New(diskdb, db.cleans, nil)
	//}
	return db
}
