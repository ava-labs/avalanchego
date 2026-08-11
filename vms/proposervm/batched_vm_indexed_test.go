// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// TestGetAncestorsIndexedMatchesSerial exercises the height-indexed path, which
// only applies to *accepted* blocks. The pre-existing GetAncestors tests verify
// their blocks without accepting them, so they exercise the parent-chasing
// fallback instead and would not catch a regression here.
func TestGetAncestorsIndexedMatchesSerial(t *testing.T) {
	require := require.New(t)
	coreVM, proVM := initTestRemoteProposerVM(t, upgradetest.Latest)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	const numBlocks = 8

	var (
		coreBlks = []*snowmantest.Block{snowmantest.Genesis}
		proBlks  = make([]snowman.Block, 0, numBlocks)
	)
	// Resolving the preferred block between builds goes through the inner VM,
	// so this must be in place for the whole loop.
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (snowman.Block, error) {
		for _, blk := range coreBlks {
			if blk.ID() == blkID {
				return blk, nil
			}
		}
		return nil, errUnknownBlock
	}
	// Once accepted, a block leaves verifiedBlocks and its inner block is
	// rebuilt from the stored bytes.
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (snowman.Block, error) {
		for _, blk := range coreBlks {
			if bytes.Equal(blk.Bytes(), b) {
				return blk, nil
			}
		}
		return nil, errUnknownBlock
	}

	for i := range numBlocks {
		coreBlk := snowmantest.BuildChild(coreBlks[i])
		coreBlks = append(coreBlks, coreBlk)
		coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
			return coreBlk, nil
		}

		proBlk, err := proVM.BuildBlock(t.Context())
		require.NoError(err)
		require.NoError(proBlk.Verify(t.Context()))
		require.NoError(proVM.SetPreference(t.Context(), proBlk.ID()))
		// Accepting is what populates the height index, and so what makes the
		// indexed path applicable.
		require.NoError(proBlk.Accept(t.Context()))
		proBlks = append(proBlks, proBlk)

		require.NoError(proVM.waitForProposerWindow())
	}

	// The inner VM must not be consulted for post-fork ancestors.
	coreVM.GetAncestorsF = func(context.Context, ids.ID, int, int, time.Duration) ([][]byte, error) {
		return nil, errUnknownBlock
	}

	tip := proBlks[len(proBlks)-1]

	// The indexed path must be the one that runs.
	_, ok := proVM.getAncestorsIndexed(
		t.Context(),
		tip.ID(),
		numBlocks,
		constants.MaxContainersLen,
		proVM.Clock.Time().Add(time.Hour),
	)
	require.True(ok, "indexed path did not apply to an accepted block")

	got, err := proVM.GetAncestors(
		t.Context(),
		tip.ID(),
		numBlocks,
		constants.MaxContainersLen,
		time.Hour,
	)
	require.NoError(err)
	require.Len(got, numBlocks)

	// Newest first, and byte-identical to the blocks that were built.
	for i, blkBytes := range got {
		require.Equal(proBlks[len(proBlks)-1-i].Bytes(), blkBytes)
	}
}

// TestGetAncestorsIndexedSkippedForUnacceptedBlock checks that a block which is
// not the accepted block at its height falls back to the serial walk rather
// than being answered from the accepted chain.
func TestGetAncestorsIndexedSkippedForUnacceptedBlock(t *testing.T) {
	require := require.New(t)
	coreVM, proVM := initTestRemoteProposerVM(t, upgradetest.Latest)
	defer func() {
		require.NoError(proVM.Shutdown(t.Context()))
	}()

	coreBlk := snowmantest.BuildChild(snowmantest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (snowman.Block, error) {
		return coreBlk, nil
	}
	proBlk, err := proVM.BuildBlock(t.Context())
	require.NoError(err)
	require.NoError(proBlk.Verify(t.Context()))
	// Deliberately not accepted.

	_, ok := proVM.getAncestorsIndexed(
		t.Context(),
		proBlk.ID(),
		10,
		constants.MaxContainersLen,
		proVM.Clock.Time().Add(time.Hour),
	)
	require.False(ok, "indexed path applied to a block that was never accepted")
}
