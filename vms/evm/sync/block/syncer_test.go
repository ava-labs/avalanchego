// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/rlp"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
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
		wantRequests  int32 // requests the syncer must send to peers
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
			nodeID := ids.GenerateTestNodeID()

			blocks := synctest.MakeChain(t, tt.numBlocks, synctest.WithTxsPerBlock(tt.txsPerBlock))
			target := rawdb.NewMemoryDatabase()
			for _, h := range tt.onDisk {
				writeBlock(t, target, blocks[h])
			}

			net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
			handler, requests := countingHandler(t, blocks)
			require.NoError(t, net.AddHandler(p2p.EVMBlockRequestHandlerID, handler))

			var verified atomic.Int32
			var opts []SyncerOption
			if tt.wantVerified > 0 {
				opts = append(opts, WithBlockVerifier(func(*types.Block) error {
					verified.Add(1)
					return nil
				}))
			}

			tip := blocks[tt.fromHeight]
			syncer, err := NewSyncer(logging.NoLog{}, NewClient(net, tracker), target, tip.Hash(), tt.fromHeight, tt.blocksToFetch, opts...)
			require.NoError(t, err)
			require.NoError(t, syncer.Sync(ctx))

			require.Equal(t, tt.wantVerified, verified.Load())
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
	_, err := NewSyncer(logging.NoLog{}, nil, db, common.Hash{}, 0, 0)
	require.ErrorIs(t, err, errBlocksToFetchRequired)

	_, err = NewSyncer(logging.NoLog{}, nil, db, common.Hash{}, 5, 3)
	require.ErrorIs(t, err, errFromHashRequired)
}

func TestSyncer_ContextCancelled(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	blocks := synctest.MakeChain(t, 10)

	ctx, cancel := context.WithCancel(t.Context())
	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, RegisterHandler(logging.NoLog{}, net, synctest.NewBlockMap(blocks)))

	tip := blocks[5]
	syncer, err := NewSyncer(logging.NoLog{}, NewClient(net, tracker), rawdb.NewMemoryDatabase(), tip.Hash(), 5, 3)
	require.NoError(t, err)

	cancel() // cancel before Sync runs
	require.ErrorIs(t, syncer.Sync(ctx), context.Canceled)
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

			net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
			handler, served := staticBlockHandler(tt.served, cancel)
			require.NoError(t, net.AddHandler(p2p.EVMBlockRequestHandlerID, handler))

			var opts []SyncerOption
			if tt.verify != nil {
				opts = append(opts, WithBlockVerifier(tt.verify))
			}
			target := rawdb.NewMemoryDatabase()
			syncer, err := NewSyncer(logging.NoLog{}, NewClient(net, tracker), target, tip.Hash(), tip.NumberU64(), 1, opts...)
			require.NoError(t, err)

			require.ErrorIs(t, syncer.Sync(ctx), context.Canceled)
			require.Nil(t, rawdb.ReadBlock(target, tip.Hash(), tip.NumberU64()))
			require.Greater(t, served.Load(), int32(1), "the response was never rejected and re-requested")
		})
	}
}

// staticBlockHandler serves blockBytes to every request and cancels on the
// second, which the syncer only sends after rejecting the first.
func staticBlockHandler(blockBytes []byte, cancel context.CancelFunc) (p2p.Handler, *atomic.Int32) {
	var served atomic.Int32
	h := p2p.TestHandler{
		AppRequestF: func(_ context.Context, _ ids.NodeID, _ time.Time, _ []byte) ([]byte, *avacommon.AppError) {
			respBytes, err := proto.Marshal(&syncpb.GetBlockResponse{Blocks: [][]byte{blockBytes}})
			if err != nil {
				return nil, avacommon.ErrUndefined
			}
			if served.Add(1) > 1 {
				cancel()
			}
			return respBytes, nil
		},
	}
	return h, &served
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

// countingHandler serves blocks and counts how many requests it receives, so a
// test can assert the syncer never asked for blocks it already had.
func countingHandler(t *testing.T, blocks []*types.Block) (p2p.Handler, *atomic.Int32) {
	log := loggingtest.New(t, logging.Debug)
	inner := handlers.NewHandler(
		log,
		func() *syncpb.GetBlockRequest { return &syncpb.GetBlockRequest{} },
		newResponder(log, synctest.NewBlockMap(blocks)),
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
