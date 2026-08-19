// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestVerifyBlocks(t *testing.T) {
	blocks := synctest.MakeChain(t, 5, synctest.WithTxsPerBlock(2))
	tip := blocks[len(blocks)-1]
	chain := encodeTipFirst(t, blocks, 3)

	tests := []struct {
		name       string
		hash       common.Hash
		numParents uint16
		raw        [][]byte
		parseFails bool
		wantErr    error
	}{
		{
			name:       "valid",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        chain,
		},
		{
			name:       "parser_rejects",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        chain,
			parseFails: true,
			wantErr:    errTestParser,
		},
		{
			name:       "empty_response",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        nil,
			wantErr:    errEmptyResponse,
		},
		{
			name:       "too_many_blocks",
			hash:       tip.Hash(),
			numParents: 1,
			raw:        chain,
			wantErr:    errTooManyBlocks,
		},
		{
			name:       "wrong_tip_breaks_the_chain",
			hash:       blocks[0].Hash(),
			numParents: 3,
			raw:        chain,
			wantErr:    errBlockHashMismatch,
		},
		{
			name:       "unparsable_block",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        [][]byte{{0xff, 0xff}},
			wantErr:    errParseBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parse := decodeBlock
			if tt.parseFails {
				parse = failingParser
			}
			got, err := verifyBlocks(tt.hash, tt.numParents, tt.raw, parse)
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
		txsPerBlock   int // non-zero grows blocks so a long sync crosses the flush threshold
		wantRequests  int // requests the syncer must send to peers
	}{
		{
			name:          "all_from_network",
			numBlocks:     10,
			fromHeight:    5,
			blocksToFetch: 3,
			wantHeights:   []int{3, 4, 5},
			wantRequests:  1,
		},
		{
			name:          "some_already_on_disk",
			numBlocks:     10,
			onDisk:        []int{4, 5},
			fromHeight:    5,
			blocksToFetch: 3,
			wantHeights:   []int{3, 4, 5},
			wantRequests:  1,
		},
		{
			name:          "all_already_on_disk",
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
			name:          "tip_missing_refetches_suffix",
			numBlocks:     10,
			onDisk:        []int{3, 4},
			fromHeight:    5,
			blocksToFetch: 3,
			wantHeights:   []int{3, 4, 5},
			wantRequests:  1,
		},
		{
			name:          "single_block",
			numBlocks:     10,
			fromHeight:    7,
			blocksToFetch: 1,
			wantHeights:   []int{7},
			wantRequests:  1,
		},
		{
			// blocksToFetch exceeds maxParentsPerRequest, so this drives more
			// than one request through the re-request loop.
			name:          "batches_across_requests",
			numBlocks:     80,
			fromHeight:    70,
			blocksToFetch: 70,
			wantHeights:   []int{1, 7, 64, 70},
			wantRequests:  2,
		},
		{
			name:          "stops_at_genesis",
			numBlocks:     10,
			fromHeight:    10,
			blocksToFetch: 30,
			wantHeights:   []int{0, 1, 5, 10},
			wantRequests:  1,
		},
		{
			name:          "flushes_a_long_sync",
			numBlocks:     400,
			txsPerBlock:   4,
			fromHeight:    400,
			blocksToFetch: 400,
			wantHeights:   []int{1, 200, 399, 400},
			wantRequests:  7,
		},
		{
			// An on-disk prefix sits behind a gap wider than one batch. The
			// skip runs inside the fetch loop, so it is not limited to the
			// leading run.
			name:          "skips_an_on_disk_prefix_behind_a_gap",
			numBlocks:     130,
			onDisk:        heights(1, 66),
			fromHeight:    130,
			blocksToFetch: 130,
			wantHeights:   []int{1, 66, 67, 130},
			wantRequests:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			blocks := synctest.MakeChain(t, tt.numBlocks, synctest.WithTxsPerBlock(tt.txsPerBlock))
			target := rawdb.NewMemoryDatabase()
			for _, h := range tt.onDisk {
				writeBlock(t, target, blocks[h])
			}

			net, tracker, requests := countingNetwork(t, ctx, blocks)

			tip := blocks[tt.fromHeight]
			syncer, err := NewSyncer(loggingtest.New(t, logging.Debug), NewClient(net, tracker), target, decodeBlock, tip.Hash(), tt.fromHeight, tt.blocksToFetch)
			require.NoError(t, err)
			require.NoError(t, syncer.Sync(ctx))

			// Skipped blocks must never be requested from peers.
			require.Len(t, requests.Requests(), tt.wantRequests)
			for _, h := range tt.wantHeights {
				want := blocks[h]
				require.NotNil(t, rawdb.ReadBlock(target, want.Hash(), want.NumberU64()), "block %d missing", h)
				require.Equal(t, want.Hash(), rawdb.ReadCanonicalHash(target, want.NumberU64()),
					"block %d is not canonical", h)
			}
		})
	}
}

func TestNewSyncer_Validation(t *testing.T) {
	log := loggingtest.New(t, logging.Debug)
	db := rawdb.NewMemoryDatabase()
	_, err := NewSyncer(log, nil, db, decodeBlock, common.Hash{}, 0, 0)
	require.ErrorIs(t, err, errBlocksToFetchRequired)

	_, err = NewSyncer(log, nil, db, decodeBlock, common.Hash{}, 5, 3)
	require.ErrorIs(t, err, errFromHashRequired)

	_, err = NewSyncer(log, nil, db, nil, common.Hash{}, 0, 3)
	require.ErrorIs(t, err, errParseBlockRequired)
}

func TestSyncer_ContextCancelled(t *testing.T) {
	// Cancelling before Sync stops the skip walk. Cancelling once a batch has
	// been accepted stops the fetch loop. Each guard sits in a different loop.
	tests := []struct {
		name             string
		cancelAfterBatch bool
	}{
		{name: "before_the_skip_walk"},
		{name: "between_batches", cancelAfterBatch: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := synctest.MakeChain(t, 200)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			net, tracker, _ := countingNetwork(t, ctx, blocks)

			// Accept the batch, then cancel, so the loop reaches its guard with
			// blocks already written.
			parse := decodeBlock
			if tt.cancelAfterBatch {
				parse = func(b []byte) (*types.Block, error) {
					cancel()
					return decodeBlock(b)
				}
			}

			target := rawdb.NewMemoryDatabase()
			tip := blocks[len(blocks)-1]
			syncer, err := NewSyncer(loggingtest.New(t, logging.Debug), NewClient(net, tracker), target, parse, tip.Hash(), tip.NumberU64(), 200)
			require.NoError(t, err)

			if !tt.cancelAfterBatch {
				cancel()
			}
			require.ErrorIs(t, syncer.Sync(ctx), context.Canceled)

			// A batch accepted before the cancellation must survive it.
			stored := rawdb.ReadBlock(target, tip.Hash(), tip.NumberU64())
			if tt.cancelAfterBatch {
				require.NotNil(t, stored, "verified blocks were discarded on cancel")
				return
			}
			require.Nil(t, stored, "nothing was fetched before the cancel")
		})
	}
}

// Every response the syncer rejects must be re-requested and must not reach disk.
func TestSyncer_RejectsBadResponse(t *testing.T) {
	blocks := synctest.MakeChain(t, 5, synctest.WithTxsPerBlock(2))
	tip := blocks[len(blocks)-1]

	tests := []struct {
		name       string
		served     []byte
		parseFails bool
	}{
		{
			name:   "block_does_not_hash_to_the_requested_tip",
			served: encodeBlock(t, blocks[0]),
		},
		{
			name:       "parser_rejects",
			served:     encodeBlock(t, tip),
			parseFails: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			// A rejection re-requests forever, so the second request signals it
			// and ends the sync. Record outside the guard, which cancels
			// before delegating.
			guard := synctest.NewCancelAfter(&staticResponder{blocks: [][]byte{tt.served}}, 2, cancel)
			recorder := synctest.NewRecordingResponder(guard)
			log := loggingtest.New(t, logging.Debug)
			net, tracker := synctest.ServeResponder(t, ctx, log, p2p.EVMBlockRequestHandlerID, recorder)

			parse := decodeBlock
			if tt.parseFails {
				parse = failingParser
			}
			target := rawdb.NewMemoryDatabase()
			syncer, err := NewSyncer(log, NewClient(net, tracker), target, parse, tip.Hash(), tip.NumberU64(), 1)
			require.NoError(t, err)

			require.ErrorIs(t, syncer.Sync(ctx), context.Canceled)
			require.Nil(t, rawdb.ReadBlock(target, tip.Hash(), tip.NumberU64()))
			require.Len(t, recorder.Requests(), 2, "the response was never rejected and re-requested")
		})
	}
}

// staticResponder answers every request with the same blocks.
type staticResponder struct {
	blocks [][]byte
}

func (r *staticResponder) Respond(context.Context, ids.NodeID, *syncpb.GetBlockRequest) (*syncpb.GetBlockResponse, *avacommon.AppError) {
	return &syncpb.GetBlockResponse{Blocks: r.blocks}, nil
}

var errTestParser = errors.New("the parser rejected the block")

type blockRecorder = synctest.RecordingResponder[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse]

func countingNetwork(t *testing.T, ctx context.Context, blocks []*types.Block) (*p2p.Network, *p2p.PeerTracker, *blockRecorder) {
	log := loggingtest.New(t, logging.Debug)
	r := synctest.NewRecordingResponder(newResponder(log, synctest.NewBlockDB(blocks)))
	net, tracker := synctest.ServeResponder(t, ctx, log, p2p.EVMBlockRequestHandlerID, r)
	return net, tracker, r
}

func writeBlock(t *testing.T, db ethdb.Database, block *types.Block) {
	t.Helper()
	batch := db.NewBatch()
	rawdb.WriteBlock(batch, block)
	rawdb.WriteCanonicalHash(batch, block.Hash(), block.NumberU64())
	require.NoError(t, batch.Write())
}

func encodeBlock(t *testing.T, block *types.Block) []byte {
	t.Helper()
	raw, err := rlp.EncodeToBytes(block)
	require.NoError(t, err)
	return raw
}

func encodeTipFirst(t *testing.T, blocks []*types.Block, n int) [][]byte {
	t.Helper()
	raw := make([][]byte, n)
	for i := range n {
		raw[i] = encodeBlock(t, blocks[len(blocks)-1-i])
	}
	return raw
}

// A peer that reorged still holds the requested block, so naming it by hash
// keeps that peer useful.
func heights(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for h := from; h <= to; h++ {
		out = append(out, h)
	}
	return out
}

// decodeBlock stands in for the chain's parser, which owns block validity.
func decodeBlock(b []byte) (*types.Block, error) {
	block := new(types.Block)
	if err := rlp.DecodeBytes(b, block); err != nil {
		return nil, err
	}
	return block, nil
}

func failingParser([]byte) (*types.Block, error) {
	return nil, errTestParser
}

// countingDB counts the reads a sync performs.
type countingDB struct {
	ethdb.Database
	reads atomic.Int32
}

func (d *countingDB) Get(key []byte) ([]byte, error) {
	d.reads.Add(1)
	return d.Database.Get(key)
}

func (d *countingDB) Has(key []byte) (bool, error) {
	d.reads.Add(1)
	return d.Database.Has(key)
}

// A cancelled context must stop the walk before it touches disk, since the
// skip path returns to the top of the loop without reaching the fetch.
func TestSyncer_CancelledBeforeSkippingOnDisk(t *testing.T) {
	blocks := synctest.MakeChain(t, 20)
	tip := blocks[len(blocks)-1]

	db := &countingDB{Database: synctest.NewBlockDB(blocks)}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	log := loggingtest.New(t, logging.Debug)
	syncer, err := NewSyncer(log, nil, db, decodeBlock, tip.Hash(), tip.NumberU64(), 20)
	require.NoError(t, err)

	require.ErrorIs(t, syncer.Sync(ctx), context.Canceled)
	require.Zero(t, db.reads.Load(), "a cancelled sync must not walk the on-disk run")
}
