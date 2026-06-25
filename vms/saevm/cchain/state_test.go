// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
)

func TestConsensusGettersAfterRestart(t *testing.T) {
	key := txtest.NewKey(t)
	alloc := withMaxAllocFor(key.EthAddress())
	db := memdb.New()

	ctx, node := newSUT(t, alloc, withDB(db))
	w := newWallet(key, node.ctx, node.Client)

	genesis, err := node.GetBlock(ctx, node.lastAccepted(ctx, t))
	require.NoErrorf(t, err, "%T.GetBlock()", node.VM)
	const numBlocks = 3
	want := make([]*blocks.Block, 0, numBlocks+1)
	want = append(want, genesis)
	for range numBlocks {
		blk := node.issueAndExecute(ctx, t, w.newMinimalTx(t))
		want = append(want, blk)
	}
	require.NoErrorf(t, node.Shutdown(ctx), "%T.Shutdown()", node.VM)

	for _, state := range []snow.State{
		snow.StateSyncing,
		snow.Bootstrapping,
		snow.NormalOp,
	} {
		t.Run(state.String(), func(t *testing.T) {
			ctx, s := newSUT(t, alloc, withDB(db), withState(state))

			wantLastAccepted := want[len(want)-1].ID()
			gotLastAccepted, err := s.LastAccepted(ctx)
			require.NoErrorf(t, err, "%T.LastAccepted()", s)
			assert.Equalf(t, wantLastAccepted, gotLastAccepted, "%T.LastAccepted()", s)

			for _, b := range want {
				gotID, err := s.GetBlockIDAtHeight(ctx, b.Height())
				require.NoErrorf(t, err, "%T.GetBlockIDAtHeight(%d)", s, b.Height())
				assert.Equalf(t, b.ID(), gotID, "%T.GetBlockIDAtHeight(%d)", s, b.Height())

				gotBlock, err := s.GetBlock(ctx, b.ID())
				require.NoErrorf(t, err, "%T.GetBlock(%s)", s, b.ID())
				assert.Equalf(t, b.ID(), gotBlock.ID(), "%T.GetBlock(%s).ID()", s, b.ID())
				assert.Equalf(t, b.Height(), gotBlock.Height(), "%T.GetBlock(%s).Height()", s, b.ID())
			}
		})
	}
}

func TestGetGenesisNoState(t *testing.T) {
	ctx, sut := newSUT(t, withState(snow.StateSyncing))
	hash, err := sut.LastAccepted(ctx)
	require.NoErrorf(t, err, "%T.LastAccepted()", sut)
	require.NotZero(t, hash)

	gotID, err := sut.GetBlockIDAtHeight(ctx, 0)
	require.NoErrorf(t, err, "%T.GetBlockIDAtHeight(%d)", sut, 0)
	assert.Equalf(t, hash, gotID, "%T.GetBlockIDAtHeight(%d)", sut, 0)

	gotBlock, err := sut.GetBlock(ctx, hash)
	require.NoErrorf(t, err, "%T.GetBlock(%s)", sut, hash)
	assert.Equalf(t, hash, gotBlock.ID(), "%T.GetBlock(%s).ID()", sut, hash)
	assert.Equalf(t, uint64(0), gotBlock.Height(), "%T.GetBlock(%s).Height()", sut, hash)
}

func TestWaitForEventInitializing(t *testing.T) {
	ctx, sut := newSUT(t, withState(snow.Initializing))

	ctx, cancel := context.WithCancel(ctx)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		_, err := sut.WaitForEvent(egCtx)
		return err
	})
	cancel()
	require.ErrorIs(t, eg.Wait(), context.Canceled)
}
