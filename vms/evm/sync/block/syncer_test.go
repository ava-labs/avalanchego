// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
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
		verify     BlockVerifier
		wantErr    error
	}{
		{
			name:       "valid",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        chain,
		},
		{
			// The block hash still matches, so only the body roots catch this.
			name:       "forged transactions",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        [][]byte{encodeBlock(t, forgeBlock(tip.Header(), types.Body{Transactions: forgedTxs}))},
			wantErr:    errTxHashMismatch,
		},
		{
			name:       "forged uncles",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        [][]byte{encodeBlock(t, forgeBlock(tip.Header(), types.Body{Uncles: forgedUncles}))},
			wantErr:    errUncleHashMismatch,
		},
		{
			name:       "chain-specific verifier rejects",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        chain,
			verify:     func(*types.Block) error { return errTestVerifier },
			wantErr:    errTestVerifier,
		},
		{
			name:       "chain-specific verifier accepts",
			hash:       tip.Hash(),
			numParents: 3,
			raw:        chain,
			verify:     func(*types.Block) error { return nil },
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
			got, err := verifyBlocks(tt.hash, tt.numParents, tt.raw, tt.verify)
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
		txsPerBlock   int   // non-zero grows blocks so a long sync crosses the flush threshold
		wantRequests  int   // requests the syncer must send to peers
		wantVerified  int32 // blocks the verifier must see
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
			// blocksToFetch exceeds maxParentsPerRequest, so this drives more
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
		{
			// Long enough to cross [ethdb.IdealBatchSize], so the batch flushes
			// mid-run.
			name:          "flushes a long sync",
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
			name:          "skips an on-disk prefix behind a gap",
			numBlocks:     130,
			onDisk:        heights(1, 66),
			fromHeight:    130,
			blocksToFetch: 130,
			wantHeights:   []int{1, 66, 67, 130},
			wantRequests:  1,
		},
		{
			name:          "accepting verifier sees every block",
			numBlocks:     10,
			fromHeight:    5,
			blocksToFetch: 3,
			wantHeights:   []int{3, 4, 5},
			wantRequests:  1,
			wantVerified:  3,
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

			var (
				verified atomic.Int32
				opts     []SyncerOption
			)
			if tt.wantVerified > 0 {
				opts = append(opts, WithBlockVerifier(func(*types.Block) error {
					verified.Add(1)
					return nil
				}))
			}

			tip := blocks[tt.fromHeight]
			syncer, err := NewSyncer(loggingtest.New(t, logging.Debug), NewClient(net, tracker), target, tip.Hash(), tt.fromHeight, tt.blocksToFetch, opts...)
			require.NoError(t, err)
			require.NoError(t, syncer.Sync(ctx))

			require.Equal(t, tt.wantVerified, verified.Load())
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
	_, err := NewSyncer(log, nil, db, common.Hash{}, 0, 0)
	require.ErrorIs(t, err, errBlocksToFetchRequired)

	_, err = NewSyncer(log, nil, db, common.Hash{}, 5, 3)
	require.ErrorIs(t, err, errFromHashRequired)
}

func TestSyncer_ContextCancelled(t *testing.T) {
	// Cancelling before Sync stops the skip walk. Cancelling once a batch has
	// been accepted stops the fetch loop. Each guard sits in a different loop.
	tests := []struct {
		name             string
		cancelAfterBatch bool
	}{
		{name: "before the skip walk"},
		{name: "between batches", cancelAfterBatch: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := synctest.MakeChain(t, 200)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			net, tracker, _ := countingNetwork(t, ctx, blocks)

			// Accept the batch, then cancel, so the loop reaches its guard with
			// blocks already written.
			var opts []SyncerOption
			if tt.cancelAfterBatch {
				opts = append(opts, WithBlockVerifier(func(*types.Block) error {
					cancel()
					return nil
				}))
			}

			target := rawdb.NewMemoryDatabase()
			tip := blocks[len(blocks)-1]
			syncer, err := NewSyncer(loggingtest.New(t, logging.Debug), NewClient(net, tracker), target, tip.Hash(), tip.NumberU64(), 200, opts...)
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
		name   string
		served []byte
		verify BlockVerifier
	}{
		{
			name:   "block does not hash to the requested tip",
			served: encodeBlock(t, blocks[0]),
		},
		{
			name:   "body does not match the header",
			served: encodeBlock(t, forgeBlock(tip.Header(), types.Body{Transactions: forgedTxs})),
		},
		{
			name:   "chain-specific verifier rejects",
			served: encodeBlock(t, tip),
			verify: func(*types.Block) error { return errTestVerifier },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			// The syncer only re-requests after rejecting, so a second request
			// is the rejection signal and ends a sync that never converges.
			// Record before cancelling. CancelAfter cancels ahead of its inner
			// responder, so recording inside it races Sync's return.
			guard := synctest.NewCancelAfter(&staticResponder{blocks: [][]byte{tt.served}}, 2, cancel)
			recorder := synctest.NewRecordingResponder(guard)
			log := loggingtest.New(t, logging.Debug)
			net, tracker := synctest.ServeResponder(t, ctx, log, p2p.EVMBlockRequestHandlerID, recorder)

			var opts []SyncerOption
			if tt.verify != nil {
				opts = append(opts, WithBlockVerifier(tt.verify))
			}
			target := rawdb.NewMemoryDatabase()
			syncer, err := NewSyncer(log, NewClient(net, tracker), target, tip.Hash(), tip.NumberU64(), 1, opts...)
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

var errTestVerifier = errors.New("chain-specific verifier rejected the block")

var (
	forgedTxs    = []*types.Transaction{types.NewTransaction(99, common.Address{0xff}, big.NewInt(1), 21_000, big.NewInt(1), nil)}
	forgedUncles = []*types.Header{{Number: big.NewInt(1), Extra: []byte{}}}
)

// forgeBlock pairs header with a body it never committed to.
func forgeBlock(header *types.Header, body types.Body) *types.Block {
	return types.NewBlockWithHeader(header).WithBody(body).WithWithdrawals(body.Withdrawals)
}

// Driven directly because these cases change the header, which verifyBlocks
// rejects on the hash before reaching verifyBody.
func TestVerifyBody_Withdrawals(t *testing.T) {
	withdrawals := []*types.Withdrawal{{Index: 1, Validator: 2, Amount: 3}}
	committed := types.DeriveSha(types.Withdrawals(withdrawals), trie.NewStackTrie(nil))
	other := common.Hash{0xab}

	tests := []struct {
		name    string
		commits *common.Hash // header WithdrawalsHash
		body    []*types.Withdrawal
		wantErr error
	}{
		{
			name:    "committed and present",
			commits: &committed,
			body:    withdrawals,
		},
		{
			name:    "committed but body has none",
			commits: &committed,
			wantErr: errMissingWithdrawals,
		},
		{
			name:    "committed to a different root",
			commits: &other,
			body:    withdrawals,
			wantErr: errWithdrawalsHashMismatch,
		},
		{
			name:    "present but header commits to none",
			body:    withdrawals,
			wantErr: errUnexpectedWithdrawals,
		},
	}

	blocks := synctest.MakeChain(t, 2)
	tip := blocks[len(blocks)-1]

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := tip.Header()
			header.WithdrawalsHash = tt.commits

			err := verifyBody(forgeBlock(header, types.Body{Withdrawals: tt.body}))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

type blockRecorder = synctest.RecordingResponder[*syncpb.GetBlockRequest, *syncpb.GetBlockResponse]

// countingNetwork serves blocks on a loopback network and counts the requests,
// so a test can assert the syncer never asked for blocks it already had.
func countingNetwork(t *testing.T, ctx context.Context, blocks []*types.Block) (*p2p.Network, *p2p.PeerTracker, *blockRecorder) {
	log := loggingtest.New(t, logging.Debug)
	r := synctest.NewRecordingResponder(newResponder(log, synctest.NewBlockMap(blocks)))
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
func TestSyncer_ServesNonCanonicalBlock(t *testing.T) {
	ctx := t.Context()

	ours := synctest.MakeChain(t, 10, synctest.WithTxsPerBlock(1))
	theirs := synctest.MakeChain(t, 10, synctest.WithTxsPerBlock(3))
	wanted := ours[5]
	require.NotEqual(t, wanted.Hash(), theirs[5].Hash(), "the chains diverge")
	require.Equal(t, wanted.NumberU64(), theirs[5].NumberU64(), "at the same height")

	// The peer is canonical on theirs but still stores ours.
	both := append(append([]*types.Block{}, theirs...), ours...)

	net, tracker, served := countingNetwork(t, ctx, both)

	got, err := getBlocks(ctx, loggingtest.New(t, logging.Debug), NewClient(net, tracker),
		wanted.Hash(), wanted.NumberU64(), 3, nil)

	require.NoError(t, err)
	require.Len(t, got, 3)
	require.Equal(t, wanted.Hash(), got[0].Hash())
	require.Len(t, served.Requests(), 1, "one request, no retry loop")
}

func heights(from, to int) []int {
	out := make([]int, 0, to-from+1)
	for h := from; h <= to; h++ {
		out = append(out, h)
	}
	return out
}
