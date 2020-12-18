// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"
	"sync"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	codecVersion = 0
)

type rcLock struct {
	lock  sync.Mutex
	count int
}

// Memory is the interface for shared memory inside a subnet
type Memory struct {
	lock  sync.Mutex
	log   logging.Logger
	codec codec.Manager
	locks map[ids.ID]*rcLock
	db    database.Database
}

// Initialize the SharedMemory
func (m *Memory) Initialize(log logging.Logger, db database.Database) error {
	c := linearcodec.NewDefault()
	manager := codec.NewDefaultManager()
	if err := manager.RegisterCodec(codecVersion, c); err != nil {
		return err
	}

	m.log = log
	m.codec = manager
	m.locks = make(map[ids.ID]*rcLock)
	m.db = db
	return nil
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
	return vdb, prefixdb.New(sharedID[:], vdb)
}

// ReleaseDatabase unlocks the provided DB
func (m *Memory) ReleaseDatabase(sharedID ids.ID) {
	lock := m.releaseLock(sharedID)
	lock.Unlock()
}

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

func (m *Memory) releaseLock(sharedID ids.ID) *sync.Mutex {
	m.lock.Lock()
	defer m.lock.Unlock()

	rc, exists := m.locks[sharedID]
	if !exists {
		panic("Attemping to free an unknown lock")
	}
	rc.count--
	if rc.count == 0 {
		delete(m.locks, sharedID)
	}
	return &rc.lock
}

// sharedID calculates the ID of the shared memory space
func (m *Memory) sharedID(id1, id2 ids.ID) ids.ID {
	if bytes.Compare(id1[:], id2[:]) == 1 {
		id1, id2 = id2, id1
	}

	combinedBytes, err := m.codec.Marshal(codecVersion, [2]ids.ID{id1, id2})
	m.log.AssertNoError(err)

	return hashing.ComputeHash256Array(combinedBytes)
}
