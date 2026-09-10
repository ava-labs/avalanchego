// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func TestVerifyBlocks(t *testing.T) {
	blocks := synctest.MakeChain(t, 5, synctest.WithTxsPerBlock(2))
	tip := blocks[len(blocks)-1]
	chain := encodeTipFirst(t, blocks, 3)

	tests := []struct {
		name       string
		hash       common.Hash
		maxBlocks  uint16
		blockBytes [][]byte
		wantErr    error
	}{
		{
			name:       "valid",
			hash:       tip.Hash(),
			maxBlocks:  3,
			blockBytes: chain,
		},
		{
			name:       "unparsable_block",
			hash:       tip.Hash(),
			maxBlocks:  3,
			blockBytes: [][]byte{{0xff, 0xff}},
			wantErr:    errParsingBlock,
		},
		{
			name:       "empty_response",
			hash:       tip.Hash(),
			maxBlocks:  3,
			blockBytes: nil,
			wantErr:    errNoBlocks,
		},
		{
			name:       "too_many_blocks",
			hash:       tip.Hash(),
			maxBlocks:  1,
			blockBytes: chain,
			wantErr:    errTooManyBlocks,
		},
		{
			name:       "wrong_tip_breaks_the_chain",
			hash:       common.Hash{'w', 'r', 'o', 'n', 'g'},
			maxBlocks:  3,
			blockBytes: chain,
			wantErr:    errUnexpectedBlockHash,
		},
		{
			// The tip is right but the next block is not its parent.
			name:      "broken_parent_link",
			hash:      tip.Hash(),
			maxBlocks: 3,
			blockBytes: [][]byte{
				encodeBlock(t, tip),
				encodeBlock(t, blocks[0]),
			},
			wantErr: errUnexpectedBlockHash,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := verifyBlocks(tt.hash, tt.maxBlocks, tt.blockBytes, decodeBlock)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr == nil {
				assert.Equal(t, tt.blockBytes, encodeBlocks(t, got))
			} else {
				assert.Nil(t, got)
			}
		})
	}
}

func TestSyncer(t *testing.T) {
	// One chain serves every case. Non-empty bodies prove the served header
	// and body RLP splice back into the original block encoding.
	blocks := synctest.MakeChain(t, 400, synctest.WithTxsPerBlock(4))

	tests := []struct {
		name          string
		onDisk        []int // block heights pre-populated in the target
		fromHeight    uint64
		blocksToFetch uint64
		wantRequests  int // requests the syncer must send to peers
	}{
		{
			name:          "all_from_network",
			fromHeight:    5,
			blocksToFetch: 3,
			wantRequests:  1,
		},
		{
			name:          "some_already_on_disk",
			onDisk:        []int{4, 5},
			fromHeight:    5,
			blocksToFetch: 3,
			wantRequests:  1,
		},
		{
			name:          "all_already_on_disk",
			onDisk:        []int{3, 4, 5},
			fromHeight:    5,
			blocksToFetch: 3,
			wantRequests:  0,
		},
		{
			// The tip is missing, so the skip stops immediately and the
			// on-disk ancestors are refetched.
			name:          "tip_missing_refetches_suffix",
			onDisk:        []int{3, 4},
			fromHeight:    5,
			blocksToFetch: 3,
			wantRequests:  1,
		},
		{
			name:          "single_block",
			fromHeight:    7,
			blocksToFetch: 1,
			wantRequests:  1,
		},
		{
			// blocksToFetch exceeds one response, so the sync spans requests.
			name:          "batches_across_requests",
			fromHeight:    70,
			blocksToFetch: 70,
			wantRequests:  2,
		},
		{
			name:          "stops_at_genesis",
			fromHeight:    10,
			blocksToFetch: 30,
			wantRequests:  1,
		},
		{
			name:          "long_sync",
			fromHeight:    400,
			blocksToFetch: 400,
			wantRequests:  7,
		},
		{
			// An on-disk run sits behind a gap. The skip runs inside the fetch
			// loop, so it is not limited to the leading run.
			name:          "skips_an_on_disk_run_behind_a_gap",
			onDisk:        heights(1, 66),
			fromHeight:    130,
			blocksToFetch: 130,
			wantRequests:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			target := rawdb.NewMemoryDatabase()
			for _, h := range tt.onDisk {
				block := blocks[h]
				rawdb.WriteBlock(target, block)
				rawdb.WriteCanonicalHash(target, block.Hash(), block.NumberU64())
			}

			log := loggingtest.New(t, logging.Debug)
			requests := synctest.NewRecordingResponder(&responder{
				log: log,
				db:  synctest.NewBlockDB(blocks),
			})
			from := blocks[tt.fromHeight]
			net, tracker := synctest.ServeResponder(
				t,
				ctx,
				log,
				p2p.EVMBlockRequestHandlerID,
				requests,
			)
			syncer := NewSyncer(
				log,
				NewClient(log, net, tracker),
				target,
				decodeBlock,
				from.Hash(),
				tt.fromHeight,
				tt.blocksToFetch,
			)
			require.NoError(t, syncer.Sync(ctx))
			assert.Len(t, requests.Requests(), tt.wantRequests)
			assertBlocksSynced(t, target, blocks, tt.fromHeight, tt.blocksToFetch)
		})
	}
}

// A cancelled sync persists the batch it verified and a restarted sync finishes
// the sync.
func TestSyncer_ResumesAfterCancellation(t *testing.T) {
	blocks := synctest.MakeChain(t, 200)
	tip := blocks[len(blocks)-1]

	log := loggingtest.New(t, logging.Debug)
	target := rawdb.NewMemoryDatabase()

	// Cancel while the first batch is being verified, so the sync stops with
	// that batch written and the rest of the chain unfetched. The network runs
	// on the test ctx so it outlives the cancellation.
	syncCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	parse := func(b []byte) (*types.Block, error) {
		cancel()
		return decodeBlock(b)
	}

	net, tracker := synctest.ServeResponder(
		t,
		t.Context(),
		log,
		p2p.EVMBlockRequestHandlerID,
		&responder{
			log: log,
			db:  synctest.NewBlockDB(blocks),
		},
	)
	syncer := NewSyncer(
		log,
		NewClient(log, net, tracker),
		target,
		parse,
		tip.Hash(),
		tip.NumberU64(),
		200,
	)
	require.ErrorIs(t, syncer.Sync(syncCtx), context.Canceled)
	assertBlocksSynced(t, target, blocks, tip.NumberU64(), maxBlocksPerResponse)

	// The restart runs on a live ctx, making the cancel in parse a no-op.
	require.NoError(t, syncer.Sync(t.Context()))
	assertBlocksSynced(t, target, blocks, tip.NumberU64(), 200)
}

// A tampered response must be rejected and re-requested until a peer serves
// honest blocks.
func TestSyncer_RetriesBadResponses(t *testing.T) {
	blocks := synctest.MakeChain(t, 5, synctest.WithTxsPerBlock(2))
	tip := blocks[len(blocks)-1]

	tests := []struct {
		name   string
		served []byte
	}{
		{
			name:   "parser_rejects",
			served: []byte{'i', 'n', 'v', 'a', 'l', 'i', 'd'},
		},
		{
			name:   "block_does_not_hash_to_the_requested_tip",
			served: encodeBlock(t, blocks[0]),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			log := loggingtest.New(t, logging.Debug)
			// The first response carries tt.served instead of the real blocks,
			// every later response is honest.
			tamperer := synctest.NewMutatingResponder(
				&responder{
					log: log,
					db:  synctest.NewBlockDB(blocks),
				},
				1,
				func(resp *syncpb.GetBlockResponse) {
					resp.Blocks = [][]byte{tt.served}
				},
			)
			recorder := synctest.NewRecordingResponder(tamperer)
			net, tracker := synctest.ServeResponder(t, ctx, log, p2p.EVMBlockRequestHandlerID, recorder)

			target := rawdb.NewMemoryDatabase()
			syncer := NewSyncer(
				log,
				NewClient(log, net, tracker),
				target,
				decodeBlock,
				tip.Hash(),
				tip.NumberU64(),
				1,
			)

			require.NoError(t, syncer.Sync(ctx))
			assert.Len(t, recorder.Requests(), 2, "should have been re-requested")
			assertBlocksSynced(t, target, blocks, tip.NumberU64(), 1)
		})
	}
}

// assertBlocksSynced asserts that every block in the half-open interval
// (fromHeight-blocksToFetch, fromHeight] is present and canonical in db.
func assertBlocksSynced(t *testing.T, db ethdb.Reader, blocks []*types.Block, fromHeight, blocksToFetch uint64) {
	t.Helper()
	for i := range min(blocksToFetch, fromHeight+1) {
		h := fromHeight - i
		wantHash := blocks[h].Hash()
		assert.NotNilf(t, rawdb.ReadBlock(db, wantHash, h), "block %d missing", h)
		assert.Equalf(t, wantHash, rawdb.ReadCanonicalHash(db, h), "block %d is not canonical", h)
	}
}

// heights lists every height in [from, to].
func heights(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for h := from; h <= to; h++ {
		out = append(out, h)
	}
	return out
}

func encodeBlock(t *testing.T, block *types.Block) []byte {
	t.Helper()
	raw, err := rlp.EncodeToBytes(block)
	require.NoError(t, err)
	return raw
}

func encodeBlocks(t *testing.T, blocks []*types.Block) [][]byte {
	t.Helper()
	raw := make([][]byte, len(blocks))
	for i, b := range blocks {
		raw[i] = encodeBlock(t, b)
	}
	return raw
}

// encodeTipFirst encodes the last n blocks, newest first.
func encodeTipFirst(t *testing.T, blocks []*types.Block, n int) [][]byte {
	t.Helper()
	raw := make([][]byte, n)
	for i := range n {
		raw[i] = encodeBlock(t, blocks[len(blocks)-1-i])
	}
	return raw
}

// decodeBlock stands in for the chain's parser, which owns block validity.
func decodeBlock(b []byte) (*types.Block, error) {
	block := new(types.Block)
	if err := rlp.DecodeBytes(b, block); err != nil {
		return nil, err
	}
	return block, nil
}
