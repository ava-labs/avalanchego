// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customrawdb

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
)

func ExampleInspectDatabase() {
	db := &stubDatabase{
		iterator: &stubIterator{},
	}

	// Extra metadata keys: (17 + 32) + (12 + 32) = 93 bytes
	WriteSnapshotBlockHash(db, common.Hash{})
	rawdb.WriteSnapshotRoot(db, common.Hash{})
	// Trie segments: (77 + 2) + 1 = 80 bytes
	_ = WriteSyncSegment(db, common.Hash{}, common.Hash{})
	// Storage tries to fetch: 76 + 1 = 77 bytes
	_ = WriteSyncStorageTrie(db, common.Hash{}, common.Hash{})
	// Code to fetch: 34 + 0 = 34 bytes
	AddCodeToFetch(db, common.Hash{})
	// Block numbers synced to: 22 + 1 = 23 bytes
	_ = WriteSyncPerformed(db, 0)

	keyPrefix := []byte(nil)
	keyStart := []byte(nil)

	err := InspectDatabase(db, keyPrefix, keyStart)
	if err != nil {
		fmt.Println(err)
	}
	// Output:
	// +-----------------+-------------------------+----------+-------+
	// |    DATABASE     |        CATEGORY         |   SIZE   | ITEMS |
	// +-----------------+-------------------------+----------+-------+
	// | Key-Value store | Headers                 | 0.00 B   |     0 |
	// | Key-Value store | Bodies                  | 0.00 B   |     0 |
	// | Key-Value store | Receipt lists           | 0.00 B   |     0 |
	// | Key-Value store | Block number->hash      | 0.00 B   |     0 |
	// | Key-Value store | Block hash->number      | 0.00 B   |     0 |
	// | Key-Value store | Transaction index       | 0.00 B   |     0 |
	// | Key-Value store | Bloombit index          | 0.00 B   |     0 |
	// | Key-Value store | Contract codes          | 0.00 B   |     0 |
	// | Key-Value store | Hash trie nodes         | 0.00 B   |     0 |
	// | Key-Value store | Path trie state lookups | 0.00 B   |     0 |
	// | Key-Value store | Path trie account nodes | 0.00 B   |     0 |
	// | Key-Value store | Path trie storage nodes | 0.00 B   |     0 |
	// | Key-Value store | Trie preimages          | 0.00 B   |     0 |
	// | Key-Value store | Account snapshot        | 0.00 B   |     0 |
	// | Key-Value store | Storage snapshot        | 0.00 B   |     0 |
	// | Key-Value store | Clique snapshots        | 0.00 B   |     0 |
	// | Key-Value store | Singleton metadata      | 93.00 B  |     2 |
	// | Light client    | CHT trie nodes          | 0.00 B   |     0 |
	// | Light client    | Bloom trie nodes        | 0.00 B   |     0 |
	// | State sync      | Trie segments           | 78.00 B  |     1 |
	// | State sync      | Storage tries to fetch  | 77.00 B  |     1 |
	// | State sync      | Code to fetch           | 34.00 B  |     1 |
	// | State sync      | Block numbers synced to | 23.00 B  |     1 |
	// +-----------------+-------------------------+----------+-------+
	// |                            TOTAL          | 305.00 B |       |
	// +-----------------+-------------------------+----------+-------+
}

type stubDatabase struct {
	ethdb.Database
	iterator *stubIterator
}

func (s *stubDatabase) NewIterator(_, _ []byte) ethdb.Iterator {
	return s.iterator
}

// AncientSize is used in [InspectDatabase] to determine the ancient sizes.
func (*stubDatabase) AncientSize(_ string) (uint64, error) {
	return 0, nil
}

func (*stubDatabase) Ancients() (uint64, error) {
	return 0, nil
}

func (*stubDatabase) Tail() (uint64, error) {
	return 0, nil
}

func (s *stubDatabase) Put(key, value []byte) error {
	s.iterator.kvs = append(s.iterator.kvs, keyValue{key: key, value: value})
	return nil
}

func (*stubDatabase) Get(_ []byte) ([]byte, error) {
	return nil, nil
}

func (*stubDatabase) ReadAncients(_ func(ethdb.AncientReaderOp) error) error {
	return nil
}

type stubIterator struct {
	ethdb.Iterator
	i   int // see [stubIterator.pos]
	kvs []keyValue
}

type keyValue struct {
	key   []byte
	value []byte
}

// pos returns the true iterator position, which is otherwise off by one because
// Next() is called _before_ usage.
func (s *stubIterator) pos() int {
	return s.i - 1
}

func (s *stubIterator) Next() bool {
	s.i++
	return s.pos() < len(s.kvs)
}

func (*stubIterator) Release() {}

func (s *stubIterator) Key() []byte {
	return s.kvs[s.pos()].key
}

func (s *stubIterator) Value() []byte {
	return s.kvs[s.pos()].value
}
