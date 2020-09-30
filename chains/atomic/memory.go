// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"
	"sync"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/codec"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type rcLock struct {
	lock  sync.Mutex
	count int
}

// Memory is the interface for shared memory inside a subnet
type Memory struct {
	lock  sync.Mutex
	log   logging.Logger
	codec codec.Codec
	locks map[[32]byte]*rcLock
	db    database.Database
}

// Initialize the SharedMemory
func (m *Memory) Initialize(log logging.Logger, db database.Database) {
	m.log = log
	m.codec = codec.NewDefault()
	m.locks = make(map[[32]byte]*rcLock)
	m.db = db
}

// NewSharedMemory returns a new SharedMemory
func (m *Memory) NewSharedMemory(id ids.ID) SharedMemory {
	return &sharedMemory{
		m:           m,
		thisChainID: id,
	}
}

// GetDatabase returns and locks the provided DB
func (m *Memory) GetDatabase(sharedID ids.ID) (*versiondb.Database, database.Database) {
	lock := m.makeLock(sharedID)
	lock.Lock()

	vdb := versiondb.New(m.db)
	return vdb, prefixdb.New(sharedID.Bytes(), vdb)
}

// ReleaseDatabase unlocks the provided DB
func (m *Memory) ReleaseDatabase(sharedID ids.ID) {
	lock := m.releaseLock(sharedID)
	lock.Unlock()
}

func (m *Memory) makeLock(sharedID ids.ID) *sync.Mutex {
	m.lock.Lock()
	defer m.lock.Unlock()

	key := sharedID.Key()
	rc, exists := m.locks[key]
	if !exists {
		rc = &rcLock{}
		m.locks[key] = rc
	}
	rc.count++
	return &rc.lock
}

func (m *Memory) releaseLock(sharedID ids.ID) *sync.Mutex {
	m.lock.Lock()
	defer m.lock.Unlock()

	key := sharedID.Key()
	rc, exists := m.locks[key]
	if !exists {
		panic("Attemping to free an unknown lock")
	}
	rc.count--
	if rc.count == 0 {
		delete(m.locks, key)
	}
	return &rc.lock
}

// sharedID calculates the ID of the shared memory space
func (m *Memory) sharedID(id1, id2 ids.ID) ids.ID {
	idKey1 := id1.Key()
	idKey2 := id2.Key()

	if bytes.Compare(idKey1[:], idKey2[:]) == 1 {
		idKey1, idKey2 = idKey2, idKey1
	}

	combinedBytes, err := m.codec.Marshal([2][32]byte{idKey1, idKey2})
	m.log.AssertNoError(err)

	return ids.NewID(hashing.ComputeHash256Array(combinedBytes))
}
