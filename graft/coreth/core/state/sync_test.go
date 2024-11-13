// Copyright 2015 The go-ethereum Authors
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
	"math/big"

	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ava-labs/coreth/triedb/hashdb"
	"github.com/ava-labs/coreth/triedb/pathdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/holiman/uint256"
)

// testAccount is the data associated with an account used by the state tests.
type testAccount struct {
	address common.Address
	balance *big.Int
	nonce   uint64
	code    []byte
}

// makeTestState create a sample test state to test node-wise reconstruction.
func makeTestState(scheme string) (ethdb.Database, Database, *triedb.Database, common.Hash, []*testAccount) {
	// Create an empty state
	config := &triedb.Config{Preimages: true}
	if scheme == rawdb.PathScheme {
		config.PathDB = pathdb.Defaults
	} else {
		config.HashDB = hashdb.Defaults
	}
	db := rawdb.NewMemoryDatabase()
	nodeDb := triedb.NewDatabase(db, config)
	sdb := NewDatabaseWithNodeDB(db, nodeDb)
	state, _ := New(types.EmptyRootHash, sdb, nil)

	// Fill it with some arbitrary data
	var accounts []*testAccount
	for i := byte(0); i < 96; i++ {
		obj := state.getOrNewStateObject(common.BytesToAddress([]byte{i}))
		acc := &testAccount{address: common.BytesToAddress([]byte{i})}

		obj.AddBalance(uint256.NewInt(uint64(11 * i)))
		acc.balance = big.NewInt(int64(11 * i))

		obj.SetNonce(uint64(42 * i))
		acc.nonce = uint64(42 * i)

		if i%3 == 0 {
			obj.SetCode(crypto.Keccak256Hash([]byte{i, i, i, i, i}), []byte{i, i, i, i, i})
			acc.code = []byte{i, i, i, i, i}
		}
		if i%5 == 0 {
			for j := byte(0); j < 5; j++ {
				hash := crypto.Keccak256Hash([]byte{i, i, i, i, i, j, j})
				obj.SetState(hash, hash)
			}
		}
		accounts = append(accounts, acc)
	}
	root, _ := state.Commit(0, false)

	// Return the generated state
	return db, sdb, nodeDb, root, accounts
}
