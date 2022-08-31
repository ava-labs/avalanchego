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

package trie

import (
	"fmt"

	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/common"
)

// makeTestTrie create a sample test trie to test node-wise reconstruction.
func makeTestTrie() (*Database, *StateTrie, map[string][]byte) {
	// Create an empty trie
	triedb := NewDatabase(memorydb.New())
	trie, _ := NewStateTrie(common.Hash{}, common.Hash{}, triedb)

	// Fill it with some arbitrary data
	content := make(map[string][]byte)
	for i := byte(0); i < 255; i++ {
		// Map the same data under multiple keys
		key, val := common.LeftPadBytes([]byte{1, i}, 32), []byte{i}
		content[string(key)] = val
		trie.Update(key, val)

		key, val = common.LeftPadBytes([]byte{2, i}, 32), []byte{i}
		content[string(key)] = val
		trie.Update(key, val)

		// Add some other data to inflate the trie
		for j := byte(3); j < 13; j++ {
			key, val = common.LeftPadBytes([]byte{j, i}, 32), []byte{j, i}
			content[string(key)] = val
			trie.Update(key, val)
		}
	}
	root, nodes, err := trie.Commit(false)
	if err != nil {
		panic(fmt.Errorf("failed to commit trie %v", err))
	}
	if err := triedb.Update(NewWithNodeSet(nodes)); err != nil {
		panic(fmt.Errorf("failed to commit db %v", err))
	}
	// Re-create the trie based on the new state
	trie, _ = NewSecure(common.Hash{}, root, triedb)
	return triedb, trie, content
}
