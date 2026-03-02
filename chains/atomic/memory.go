// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

type rcLock struct {
	lock  sync.Mutex
	count int
}

// Memory is used to set up a bidirectional communication channel between a pair
// of chains.
//
// For any such pair, we compute a hash of the ordered pair of IDs to use as a
// prefix DB that can be shared across the two chains. On top of the prefix DB
// shared among two chains, we use constant prefixes to determine the
// inbound/outbound and value/index database assignments.
type Memory struct {
	lock  sync.Mutex
	locks map[ids.ID]*rcLock
	db    database.Database
}

func NewMemory(db database.Database) *Memory {
	return &Memory{
		locks: make(map[ids.ID]*rcLock),
		db:    db,
	}
}

func (m *Memory) NewSharedMemory(chainID ids.ID) SharedMemory {
	return &sharedMemory{
		m:           m,
		thisChainID: chainID,
	}
}

// GetSharedDatabase returns a new locked prefix db on top of an existing
// database
//
// Invariant: ReleaseSharedDatabase must be called after to free the database
// associated with [sharedID]
func (m *Memory) GetSharedDatabase(db database.Database, sharedID ids.ID) database.Database {
	lock := m.makeLock(sharedID)
	lock.Lock()
	return prefixdb.NewNested(sharedID[:], db)
}

// ReleaseSharedDatabase unlocks the provided DB
//
// Note: ReleaseSharedDatabase must be called only after a corresponding call to
// GetSharedDatabase. If ReleaseSharedDatabase is called without a corresponding
// one-to-one call with GetSharedDatabase, it will panic.
func (m *Memory) ReleaseSharedDatabase(sharedID ids.ID) {
	lock := m.releaseLock(sharedID)
	lock.Unlock()
}

// makeLock returns the lock associated with [sharedID], or creates a new one if
// it doesn't exist yet, and increments the reference count.
func (m *Memory) makeLock(sharedID ids.ID) *sync.Mutex {
	m.lock.Lock()
	defer m.lock.Unlock()

	rc, exists := m.locks[sharedID]
	if !exists {
		rc = &rcLock{}
		m.locks[sharedID] = rc
	}
	rc.count++
	return &rc.lock
}

// releaseLock returns the lock associated with [sharedID] and decrements its
// reference count. If this brings the count to 0, it will remove the lock from
// the internal map of locks. If there is no lock associated with [sharedID],
// releaseLock will panic.
func (m *Memory) releaseLock(sharedID ids.ID) *sync.Mutex {
	m.lock.Lock()
	defer m.lock.Unlock()

	rc, exists := m.locks[sharedID]
	if !exists {
		panic("attempting to free an unknown lock")
	}
	rc.count--
	if rc.count == 0 {
		delete(m.locks, sharedID)
	}
	return &rc.lock
}

// sharedID calculates the ID of the shared memory space
func sharedID(id1, id2 ids.ID) ids.ID {
	// Swap IDs locally to ensure id1 <= id2.
	if bytes.Compare(id1[:], id2[:]) == 1 {
		id1, id2 = id2, id1
	}

	combinedBytes, err := Codec.Marshal(CodecVersion, [2]ids.ID{id1, id2})
	if err != nil {
		panic(err)
	}
	return hashing.ComputeHash256Array(combinedBytes)
}
