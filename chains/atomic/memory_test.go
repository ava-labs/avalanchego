// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"testing"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/logging"
)

var (
	blockchainID0 = ids.Empty.Prefix(0)
	blockchainID1 = ids.Empty.Prefix(1)
)

func TestSharedMemorySharedID(t *testing.T) {
	sm := SharedMemory{}
	sm.Initialize(logging.NoLog{}, memdb.New())

	sharedID0 := sm.sharedID(blockchainID0, blockchainID1)
	sharedID1 := sm.sharedID(blockchainID1, blockchainID0)

	if !sharedID0.Equals(sharedID1) {
		t.Fatalf("SharedMemory.sharedID should be communitive")
	}
}

func TestSharedMemoryMakeReleaseLock(t *testing.T) {
	sm := SharedMemory{}
	sm.Initialize(logging.NoLog{}, memdb.New())

	sharedID := sm.sharedID(blockchainID0, blockchainID1)

	lock0 := sm.makeLock(sharedID)

	if lock1 := sm.makeLock(sharedID); lock0 != lock1 {
		t.Fatalf("SharedMemory.makeLock should have returned the same lock")
	}
	sm.releaseLock(sharedID)

	if lock2 := sm.makeLock(sharedID); lock0 != lock2 {
		t.Fatalf("SharedMemory.makeLock should have returned the same lock")
	}
	sm.releaseLock(sharedID)
	sm.releaseLock(sharedID)

	if lock3 := sm.makeLock(sharedID); lock0 == lock3 {
		t.Fatalf("SharedMemory.releaseLock should have returned freed the lock")
	}
	sm.releaseLock(sharedID)
}

func TestSharedMemoryUnknownFree(t *testing.T) {
	sm := SharedMemory{}
	sm.Initialize(logging.NoLog{}, memdb.New())

	sharedID := sm.sharedID(blockchainID0, blockchainID1)

	defer func() {
		if recover() == nil {
			t.Fatalf("Should have panicked due to an unknown free")
		}
	}()

	sm.releaseLock(sharedID)
}
