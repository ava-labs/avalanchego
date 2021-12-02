// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
)

var (
	blockchainID0 = ids.Empty.Prefix(0)
	blockchainID1 = ids.Empty.Prefix(1)
)

func TestMemorySharedID(t *testing.T) {
	m := Memory{}
	err := m.Initialize(logging.NoLog{}, memdb.New())
	if err != nil {
		t.Fatal(err)
	}

	sharedID0 := m.sharedID(blockchainID0, blockchainID1)
	sharedID1 := m.sharedID(blockchainID1, blockchainID0)

	if sharedID0 != sharedID1 {
		t.Fatalf("SharedMemory.sharedID should be communitive")
	}
}

func TestMemoryMakeReleaseLock(t *testing.T) {
	m := Memory{}
	err := m.Initialize(logging.NoLog{}, memdb.New())
	if err != nil {
		t.Fatal(err)
	}

	sharedID := m.sharedID(blockchainID0, blockchainID1)

	lock0 := m.makeLock(sharedID)

	if lock1 := m.makeLock(sharedID); lock0 != lock1 {
		t.Fatalf("Memory.makeLock should have returned the same lock")
	}
	m.releaseLock(sharedID)

	if lock2 := m.makeLock(sharedID); lock0 != lock2 {
		t.Fatalf("Memory.makeLock should have returned the same lock")
	}
	m.releaseLock(sharedID)
	m.releaseLock(sharedID)

	if lock3 := m.makeLock(sharedID); lock0 == lock3 {
		t.Fatalf("Memory.releaseLock should have returned freed the lock")
	}
	m.releaseLock(sharedID)
}

func TestMemoryUnknownFree(t *testing.T) {
	m := Memory{}
	err := m.Initialize(logging.NoLog{}, memdb.New())
	if err != nil {
		t.Fatal(err)
	}

	sharedID := m.sharedID(blockchainID0, blockchainID1)

	defer func() {
		if recover() == nil {
			t.Fatalf("Should have panicked due to an unknown free")
		}
	}()

	m.releaseLock(sharedID)
}
