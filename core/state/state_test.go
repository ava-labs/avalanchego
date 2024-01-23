// (c) 2019-2020, Ava Labs, Inc.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2014 The go-ethereum Authors
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
	"bytes"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/core/rawdb"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/trie"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
)

type stateTest struct {
	db    ethdb.Database
	state *StateDB
}

func newStateTest() *stateTest {
	db := rawdb.NewMemoryDatabase()
	sdb, _ := New(types.EmptyRootHash, NewDatabase(db), nil)
	return &stateTest{db: db, state: sdb}
}

func TestIterativeDump(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sdb, _ := New(types.EmptyRootHash, NewDatabaseWithConfig(db, &trie.Config{Preimages: true}), nil)
	s := &stateTest{db: db, state: sdb}

	// generate a few entries
	obj1 := s.state.GetOrNewStateObject(common.BytesToAddress([]byte{0x01}))
	obj1.AddBalance(big.NewInt(22))
	obj2 := s.state.GetOrNewStateObject(common.BytesToAddress([]byte{0x01, 0x02}))
	obj2.SetCode(crypto.Keccak256Hash([]byte{3, 3, 3, 3, 3, 3, 3}), []byte{3, 3, 3, 3, 3, 3, 3})
	obj3 := s.state.GetOrNewStateObject(common.BytesToAddress([]byte{0x02}))
	obj3.SetBalance(big.NewInt(44))
	obj4 := s.state.GetOrNewStateObject(common.BytesToAddress([]byte{0x00}))
	obj4.AddBalance(big.NewInt(1337))

	// write some of them to the trie
	s.state.updateStateObject(obj1)
	s.state.updateStateObject(obj2)
	s.state.Commit(false, false)

	b := &bytes.Buffer{}
	s.state.IterativeDump(nil, json.NewEncoder(b))
	// check that DumpToCollector contains the state objects that are in trie
	got := b.String()
	want := `{"root":"0xd5710ea8166b7b04bc2bfb129d7db12931cee82f75ca8e2d075b4884322bf3de"}
{"balance":"22","nonce":0,"root":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","codeHash":"0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470","address":"0x0000000000000000000000000000000000000001","key":"0x1468288056310c82aa4c01a7e12a10f8111a0560e72b700555479031b86c357d"}
{"balance":"1337","nonce":0,"root":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","codeHash":"0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470","address":"0x0000000000000000000000000000000000000000","key":"0x5380c7b7ae81a58eb98d9c78de4a1fd7fd9535fc953ed2be602daaa41767312a"}
{"balance":"0","nonce":0,"root":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","codeHash":"0x87874902497a5bb968da31a2998d8f22e949d1ef6214bcdedd8bae24cca4b9e3","code":"0x03030303030303","address":"0x0000000000000000000000000000000000000102","key":"0xa17eacbc25cda025e81db9c5c62868822c73ce097cee2a63e33a2e41268358a1"}
{"balance":"44","nonce":0,"root":"0x56e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421","codeHash":"0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470","address":"0x0000000000000000000000000000000000000002","key":"0xd52688a8f926c816ca1e079067caba944f158e764817b83fc43594370ca9cf62"}
`
	if got != want {
		t.Errorf("DumpToCollector mismatch:\ngot: %s\nwant: %s\n", got, want)
	}
}
