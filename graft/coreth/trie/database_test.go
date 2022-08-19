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
	"math/rand"
	"testing"
	"time"

	"github.com/ava-labs/coreth/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
)

// Tests that the trie database returns a missing trie node error if attempting
// to retrieve the meta root.
func TestDatabaseMetarootFetch(t *testing.T) {
	db := NewDatabase(memorydb.New())
	if _, err := db.RawNode(common.Hash{}); err == nil {
		t.Fatalf("metaroot retrieval succeeded")
	}
}

// Tests that calling dereference does not interfere with a concurrent commit on
// a trie with the same underlying trieDB.
func TestDereferenceWhileCommit(t *testing.T) {
	var (
		numKeys = 10
		keyLen  = 10
		db      = NewDatabase(memorydb.New())
	)

	// set up a database with a small trie
	tr1 := NewEmpty(db)
	rand.Seed(1) // set random seed so we get deterministic key/values
	FillTrie(t, numKeys, keyLen, tr1)
	root, _, err := tr1.Commit(nil, false)
	assert.NoError(t, err)
	assert.NotZero(t, root)
	db.Reference(root, common.Hash{}, true)

	// call Dereference from onleafs to simulate
	// this occurring concurrently.
	// the second trie has one more leaf so it
	// does not share the same root as the first trie.
	firstLeaf := true
	tr2 := NewEmpty(db)
	rand.Seed(1) // set random seed so we get deterministic key/values
	FillTrie(t, numKeys+1, keyLen, tr2)
	done := make(chan struct{})
	onleaf := func([][]byte, []byte, []byte, common.Hash, []byte) error {
		if firstLeaf {
			go func() {
				db.Dereference(root)
				close(done)
			}()
			select {
			case <-done:
				t.Fatal("Dereference succeeded within leaf callback")
			case <-time.After(time.Second):
			}
			firstLeaf = false
		}
		return nil
	}
	root2, _, err := tr2.Commit(onleaf, false)
	assert.NoError(t, err)
	assert.NotEqual(t, root, root2)
	db.Reference(root2, common.Hash{}, true)

	// wait for the goroutine to exit.
	<-done

	// expected behavior is for root2 to
	// be present and the trie should iterate
	// without missing nodes.
	tr3, err := New(common.Hash{}, root2, db)
	assert.NoError(t, err)
	it := tr3.NodeIterator(nil)
	for it.Next(true) {
	}
	assert.NoError(t, it.Error())
}
