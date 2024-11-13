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

package tests

import (
	"github.com/ava-labs/coreth/core/rawdb"
	"github.com/ava-labs/coreth/core/state"
	"github.com/ava-labs/coreth/core/state/snapshot"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/triedb"
	"github.com/ava-labs/coreth/triedb/hashdb"
	"github.com/ava-labs/coreth/triedb/pathdb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/holiman/uint256"
)

// StateTestState groups all the state database objects together for use in tests.
type StateTestState struct {
	StateDB   *state.StateDB
	TrieDB    *triedb.Database
	Snapshots *snapshot.Tree
}

// MakePreState creates a state containing the given allocation.
func MakePreState(db ethdb.Database, accounts types.GenesisAlloc, snapshotter bool, scheme string) StateTestState {
	tconf := &triedb.Config{Preimages: true}
	if scheme == rawdb.HashScheme {
		tconf.HashDB = hashdb.Defaults
	} else {
		tconf.PathDB = pathdb.Defaults
	}
	triedb := triedb.NewDatabase(db, tconf)
	sdb := state.NewDatabaseWithNodeDB(db, triedb)
	statedb, _ := state.New(types.EmptyRootHash, sdb, nil)
	for addr, a := range accounts {
		statedb.SetCode(addr, a.Code)
		statedb.SetNonce(addr, a.Nonce)
		statedb.SetBalance(addr, uint256.MustFromBig(a.Balance))
		for k, v := range a.Storage {
			statedb.SetState(addr, k, v)
		}
	}
	// Commit and re-open to start with a clean state.
	root, _ := statedb.Commit(0, false)

	// If snapshot is requested, initialize the snapshotter and use it in state.
	var snaps *snapshot.Tree
	if snapshotter {
		snapconfig := snapshot.Config{
			CacheSize:  1,
			NoBuild:    false,
			AsyncBuild: false,
			SkipVerify: true,
		}
		snaps, _ = snapshot.New(snapconfig, db, triedb, common.Hash{}, root)
	}
	statedb, _ = state.New(root, sdb, snaps)
	return StateTestState{statedb, triedb, snaps}
}

// Close should be called when the state is no longer needed, ie. after running the test.
func (st *StateTestState) Close() {
	if st.TrieDB != nil {
		st.TrieDB.Close()
		st.TrieDB = nil
	}
	if st.Snapshots != nil {
		// Need to call Disable here to quit the snapshot generator goroutine.
		st.Snapshots.AbortGeneration()
		st.Snapshots.Release()
		st.Snapshots = nil
	}
}
