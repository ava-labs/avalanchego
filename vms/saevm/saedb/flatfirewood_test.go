// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saedb

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/flatfirewood"
)

func TestFlatFirewoodConfig(t *testing.T) {
	cfg := Config{CommitInterval: 1, Scheme: flatfirewood.Scheme}
	require.NoError(t, cfg.Verify())
	require.True(t, cfg.IsFirewood())
	cfg.Archival = true
	require.ErrorIs(t, cfg.Verify(), errFlatArchival)
}

// TestFlatFirewoodServesPrunedRoots commits more blocks than Firewood keeps
// revisions for and checks that [Tracker.StateDB] still opens every root,
// including one that repeats after empty blocks, while unknown roots fail.
func TestFlatFirewoodServesPrunedRoots(t *testing.T) {
	cfg := Config{CommitInterval: 1, Scheme: flatfirewood.Scheme}
	tr, err := NewTracker(rawdb.NewMemoryDatabase(), cfg, types.EmptyRootHash, t.TempDir(), loggingtest.New(t, logging.Debug))
	require.NoError(t, err)

	addr := common.Address{1}
	root := types.EmptyRootHash
	var roots []common.Hash
	for block := uint64(0); block < 5; block++ {
		sdb, err := tr.StateDB(root)
		require.NoError(t, err)
		sdb.SetBalance(addr, uint256.NewInt(block+1))
		root, err = sdb.Commit(block, true)
		require.NoError(t, err)
		require.NoError(t, tr.BlockExecuted(root, root, block))
		roots = append(roots, root)
	}
	// Empty blocks keep the root; nothing reaches the trie database.
	require.NoError(t, tr.BlockExecuted(root, root, 5))
	require.NoError(t, tr.BlockExecuted(root, root, 6))
	defer func() { require.NoError(t, tr.Close(root)) }()

	for h, r := range roots {
		sdb, err := tr.StateDB(r)
		require.NoError(t, err, "StateDB(root of block %d)", h)
		require.Equal(t, uint64(h+1), sdb.GetBalance(addr).Uint64(), "balance at block %d", h)
	}
	var missing *trie.MissingNodeError
	_, err = tr.StateDB(common.Hash{0xff})
	require.ErrorAs(t, err, &missing)
}
