// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestVerifyBlocks(t *testing.T) {
	blocks := synctest.MakeChain(t, 5)
	tip := blocks[len(blocks)-1]
	chain := encodeTipFirst(t, blocks, 3)

	tests := []struct {
		name       string
		hash       common.Hash
		numParents uint16
		raw        [][]byte
		wantErr    error
	}{
		{
			name:       "valid",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        chain,
		},
		{
			name:       "empty response",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        nil,
			wantErr:    errEmptyResponse,
		},
		{
			name:       "too many blocks",
			hash:       tip.Hash(),
			numParents: 1,
			raw:        chain,
			wantErr:    errTooManyBlocks,
		},
		{
			name:       "wrong tip breaks the chain",
			hash:       blocks[0].Hash(),
			numParents: 3,
			raw:        chain,
			wantErr:    errBlockHashMismatch,
		},
		{
			name:       "undecodable block",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        [][]byte{{0xff, 0xff}},
			wantErr:    errDecodeBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := verifyBlocks(tt.hash, tt.numParents, tt.raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, len(tt.raw))
			for i, b := range got {
				require.Equal(t, blocks[len(blocks)-1-i].Hash(), b.Hash())
			}
		})
	}
}

func TestSyncer(t *testing.T) {
	tests := []struct {
		name          string
		numBlocks     int
		onDisk        []int // block heights pre-populated in the target
		fromHeight    uint64
		blocksToFetch uint64
		wantHeights   []int
		wantRequests  int32 // requests the syncer must send to peers
	}{
		{
			name:          "all from network",
			numBlocks:     10,
			fromHeight:    5,
			blocksToFetch: 3,
			wantHeights:   []int{3, 4, 5},
			wantRequests:  1,
		},
		{
			name:          "some already on disk",
			numBlocks:     10,
			onDisk:        []int{4, 5},
			fromHeight:    5,
			blocksToFetch: 3,
			wantHeights:   []int{3, 4, 5},
			wantRequests:  1,
		},
		{
			name:          "all already on disk",
			numBlocks:     10,
			onDisk:        []int{3, 4, 5},
			fromHeight:    5,
			blocksToFetch: 3,
			wantHeights:   []int{3, 4, 5},
			wantRequests:  0,
		},
		{
			// The tip is missing, so the skip stops immediately and the
			// on-disk ancestors are refetched.
			name:          "tip missing refetches suffix",
			numBlocks:     10,
			onDisk:        []int{3, 4},
			fromHeight:    5,
			blocksToFetch: 3,
			wantHeights:   []int{3, 4, 5},
			wantRequests:  1,
		},
		{
			name:          "single block",
			numBlocks:     10,
			fromHeight:    7,
			blocksToFetch: 1,
			wantHeights:   []int{7},
			wantRequests:  1,
		},
		{
			// blocksToFetch exceeds MaxParentsPerRequest, so this drives more
			// than one request through the re-request loop.
			name:          "batches across requests",
			numBlocks:     80,
			fromHeight:    70,
			blocksToFetch: 70,
			wantHeights:   []int{1, 7, 64, 70},
			wantRequests:  2,
		},
		{
			name:          "stops at genesis",
			numBlocks:     10,
			fromHeight:    10,
			blocksToFetch: 30,
			wantHeights:   []int{0, 1, 5, 10},
			wantRequests:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			nodeID := ids.GenerateTestNodeID()

			blocks := synctest.MakeChain(t, tt.numBlocks)
			target := rawdb.NewMemoryDatabase()
			for _, h := range tt.onDisk {
				writeBlock(target, blocks[h])
			}

			net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
			handler, requests := countingHandler(blocks)
			require.NoError(t, net.AddHandler(p2p.EVMBlockRequestHandlerID, handler))

			tip := blocks[tt.fromHeight]
			syncer, err := NewSyncer(NewClient(net, tracker), target, tip.Hash(), tt.fromHeight, tt.blocksToFetch)
			require.NoError(t, err)
			require.NoError(t, syncer.Sync(ctx))

			// Skipped blocks must never be requested from peers.
			require.Equal(t, tt.wantRequests, requests.Load())
			for _, h := range tt.wantHeights {
				want := blocks[h]
				require.NotNil(t, rawdb.ReadBlock(target, want.Hash(), want.NumberU64()), "block %d missing", h)
			}
		})
	}
}

func TestNewSyncer_Validation(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	_, err := NewSyncer(nil, db, common.Hash{}, 0, 0)
	require.ErrorIs(t, err, errBlocksToFetchRequired)

	_, err = NewSyncer(nil, db, common.Hash{}, 5, 3)
	require.ErrorIs(t, err, errFromHashRequired)
}

func TestSyncer_ContextCancelled(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	blocks := synctest.MakeChain(t, 10)

	ctx, cancel := context.WithCancel(t.Context())
	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, RegisterHandler(net, logging.NoLog{}, synctest.NewBlockMap(blocks)))

	tip := blocks[5]
	syncer, err := NewSyncer(NewClient(net, tracker), rawdb.NewMemoryDatabase(), tip.Hash(), 5, 3)
	require.NoError(t, err)

	cancel() // cancel before Sync runs
	require.ErrorIs(t, syncer.Sync(ctx), context.Canceled)
}

func TestSyncer_RejectsTamperedResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	nodeID := ids.GenerateTestNodeID()

	blocks := synctest.MakeChain(t, 5)
	tip := blocks[len(blocks)-1]

	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, net.AddHandler(p2p.EVMBlockRequestHandlerID, tamperingHandler(t, blocks[0])))

	got, err := getBlocks(ctx, NewClient(net, tracker), tip.Hash(), tip.NumberU64(), 3)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, got, "tampered blocks must never be accepted")
}

// tamperingHandler returns a well-formed block whose hash never matches the
// requested tip, so verification always fails.
func tamperingHandler(t *testing.T, wrong *types.Block) p2p.Handler {
	t.Helper()
	wrongBytes, err := rlp.EncodeToBytes(wrong)
	require.NoError(t, err)

	return p2p.TestHandler{
		AppRequestF: func(_ context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *avacommon.AppError) {
			req := &syncpb.GetBlockRequest{}
			if err := proto.Unmarshal(requestBytes, req); err != nil {
				return nil, avacommon.ErrUndefined
			}
			respBytes, err := proto.Marshal(&syncpb.GetBlockResponse{Blocks: [][]byte{wrongBytes}})
			if err != nil {
				return nil, avacommon.ErrUndefined
			}
			return respBytes, nil
		},
	}
}

// countingHandler serves blocks and counts how many requests it receives, so a
// test can assert the syncer never asked for blocks it already had.
func countingHandler(blocks []*types.Block) (p2p.Handler, *atomic.Int32) {
	inner := handlers.NewHandler(
		logging.NoLog{},
		func() *syncpb.GetBlockRequest { return &syncpb.GetBlockRequest{} },
		newResponder(synctest.NewBlockMap(blocks)),
	)
	var requests atomic.Int32
	h := p2p.TestHandler{
		AppRequestF: func(c context.Context, n ids.NodeID, d time.Time, b []byte) ([]byte, *avacommon.AppError) {
			requests.Add(1)
			return inner.AppRequest(c, n, d, b)
		},
	}
	return h, &requests
}

func writeBlock(db ethdb.Database, block *types.Block) {
	batch := db.NewBatch()
	rawdb.WriteBlock(batch, block)
	rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
	_ = batch.Write()
}

func encodeTipFirst(t *testing.T, blocks []*types.Block, n int) [][]byte {
	t.Helper()
	raw := make([][]byte, n)
	for i := range n {
		b, err := rlp.EncodeToBytes(blocks[len(blocks)-1-i])
		require.NoError(t, err)
		raw[i] = b
	}
	return raw
}
