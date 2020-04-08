// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"
	"sync"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/vms/components/codec"
)

type rcLock struct {
	lock  sync.Mutex
	count int
}

// SharedMemory is the interface for shared memory inside a subnet
type SharedMemory struct {
	lock  sync.Mutex
	log   logging.Logger
	codec codec.Codec
	locks map[[32]byte]*rcLock
	db    database.Database
}

// Initialize the SharedMemory
func (sm *SharedMemory) Initialize(log logging.Logger, db database.Database) {
	sm.log = log
	sm.codec = codec.NewDefault()
	sm.locks = make(map[[32]byte]*rcLock)
	sm.db = db
}

// NewBlockchainSharedMemory returns a new BlockchainSharedMemory
func (sm *SharedMemory) NewBlockchainSharedMemory(id ids.ID) *BlockchainSharedMemory {
	return &BlockchainSharedMemory{
		blockchainID: id,
		sm:           sm,
	}
}

// GetDatabase returns and locks the provided DB
func (sm *SharedMemory) GetDatabase(id ids.ID) database.Database {
	lock := sm.makeLock(id)
	lock.Lock()

	return prefixdb.New(id.Bytes(), sm.db)
}

// ReleaseDatabase unlocks the provided DB
func (sm *SharedMemory) ReleaseDatabase(id ids.ID) {
	lock := sm.releaseLock(id)
	lock.Unlock()
}

func (sm *SharedMemory) makeLock(id ids.ID) *sync.Mutex {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	key := id.Key()
	rc, exists := sm.locks[key]
	if !exists {
		rc = &rcLock{}
		sm.locks[key] = rc
	}
	rc.count++
	return &rc.lock
}

func (sm *SharedMemory) releaseLock(id ids.ID) *sync.Mutex {
	sm.lock.Lock()
	defer sm.lock.Unlock()

	key := id.Key()
	rc, exists := sm.locks[key]
	if !exists {
		panic("Attemping to free an unknown lock")
	}
	rc.count--
	if rc.count == 0 {
		delete(sm.locks, key)
	}
	return &rc.lock
}

// sharedID calculates the ID of the shared memory space
func (sm *SharedMemory) sharedID(id1, id2 ids.ID) ids.ID {
	idKey1 := id1.Key()
	idKey2 := id2.Key()

	if bytes.Compare(idKey1[:], idKey2[:]) == 1 {
		idKey1, idKey2 = idKey2, idKey1
	}

	combinedBytes, err := sm.codec.Marshal([2][32]byte{idKey1, idKey2})
	sm.log.AssertNoError(err)

	return ids.NewID(hashing.ComputeHash256Array(combinedBytes))
}
