// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	blockchainID0 = ids.Empty.Prefix(0)
	blockchainID1 = ids.Empty.Prefix(1)
)

func TestSharedID(t *testing.T) {
	sharedID0 := sharedID(blockchainID0, blockchainID1)
	sharedID1 := sharedID(blockchainID1, blockchainID0)

	require.Equal(t, sharedID0, sharedID1)
}

func TestMemoryMakeReleaseLock(t *testing.T) {
	require := require.New(t)

	m := NewMemory(memdb.New())

	sharedID := sharedID(blockchainID0, blockchainID1)

	lock0 := m.makeLock(sharedID)

	require.Equal(lock0, m.makeLock(sharedID))
	m.releaseLock(sharedID)

	require.Equal(lock0, m.makeLock(sharedID))
	m.releaseLock(sharedID)
	m.releaseLock(sharedID)

	require.Equal(lock0, m.makeLock(sharedID))
	m.releaseLock(sharedID)
}

func TestMemoryUnknownFree(t *testing.T) {
	m := NewMemory(memdb.New())

	sharedID := sharedID(blockchainID0, blockchainID1)

	defer func() {
		require.NotNil(t, recover())
	}()

	m.releaseLock(sharedID)
}
