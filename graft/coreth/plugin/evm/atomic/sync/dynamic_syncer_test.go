// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"math/rand"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic/state"
	"github.com/ava-labs/avalanchego/graft/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
)

// TestAtomicDynamicSyncer_CompletesWithoutPivot verifies that the dynamic
// wrapper completes a full sync when no pivot is triggered.
func TestAtomicDynamicSyncer_CompletesWithoutPivot(t *testing.T) {
	targetHeight := 2 * uint64(testCommitInterval)
	r := rand.New(rand.NewSource(1))
	serverTrieDB := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	root, _, _ := synctest.GenerateIndependentTrie(t, r, serverTrieDB, int(targetHeight), state.TrieKeyLength)

	ctx, mockClient, atomicBackend, clientDB := setupTestInfrastructure(t, serverTrieDB)
	atomicTrie := atomicBackend.AtomicTrie()

	inner, err := NewSyncer(mockClient, clientDB, atomicTrie, root, targetHeight)
	require.NoError(t, err)

	ds := NewAtomicDynamicSyncer(inner, mockClient, clientDB, atomicTrie, root, targetHeight)

	require.NoError(t, ds.Sync(ctx))
	require.Equal(t, targetHeight, ds.TargetHeight())
}

// TestAtomicDynamicSyncer_UpdateTarget_StaleIgnored verifies that UpdateTarget
// with a height at or below the current desired height is a no-op.
func TestAtomicDynamicSyncer_UpdateTarget_StaleIgnored(t *testing.T) {
	ds := types.NewDynamicSyncer("test", "test", &synctest.PivotSession{}, common.Hash{1}, 100)

	require.NoError(t, ds.UpdateTarget(&synctest.SyncTarget{BlockRoot: common.Hash{2}, BlockHeight: 100}))
	require.Equal(t, common.Hash{1}, ds.DesiredRoot())

	require.NoError(t, ds.UpdateTarget(&synctest.SyncTarget{BlockRoot: common.Hash{3}, BlockHeight: 50}))
	require.Equal(t, common.Hash{1}, ds.DesiredRoot())
	require.Equal(t, uint64(100), ds.TargetHeight())
}
