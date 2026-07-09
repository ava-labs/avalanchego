// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/trie"
	"github.com/ava-labs/libevm/triedb"
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

func TestVerifyLeafs(t *testing.T) {
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 50)
	r := newResponder(trieDB, common.HashLength, nil)

	partial, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{RootHash: root.Bytes(), KeyLimit: 20})
	require.NoError(t, err)
	require.NotEmpty(t, partial.ProofVals, "partial range must carry a proof")

	whole, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{RootHash: root.Bytes(), KeyLimit: 50})
	require.NoError(t, err)
	require.Empty(t, whole.ProofVals, "whole trie needs no proof")

	tampered := proto.Clone(partial).(*syncpb.GetLeafResponse)
	tampered.Values[0] = bytes.Repeat([]byte{0xff}, common.HashLength)

	tests := []struct {
		name     string
		resp     *syncpb.GetLeafResponse
		wantMore bool
		wantErr  error
	}{
		{name: "partial has more", resp: partial, wantMore: true},
		{name: "whole has no more", resp: whole},
		{name: "tampered value fails the proof", resp: tampered, wantErr: errInvalidRangeProof},
		{name: "empty without proof", resp: &syncpb.GetLeafResponse{}, wantErr: errEmptyLeafResponse},
		{name: "too many leaves", resp: &syncpb.GetLeafResponse{Keys: make([][]byte, MaxLeavesLimit+1)}, wantErr: errTooManyLeaves},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			more, err := verifyLeafs(root, nil, tt.resp)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMore, more)
		})
	}
}

func TestSyncer(t *testing.T) {
	tests := []struct {
		name         string
		numKeys      int
		wantRequests int32
	}{
		{name: "single batch", numKeys: 50, wantRequests: 1},
		{name: "exact limit", numKeys: int(MaxLeavesLimit), wantRequests: 1},
		{name: "multiple batches", numKeys: int(MaxLeavesLimit) + 50, wantRequests: 2},
		// Crosses IdealBatchSize, forcing a mid-sync flush.
		{name: "spans batch flush", numKeys: 5000, wantRequests: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			nodeID := ids.GenerateTestNodeID()

			trieDB := synctest.NewTrieDB()
			root, keys, vals := synctest.FillTrie(t, trieDB, tt.numKeys)

			net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
			handler, requests := countingLeafHandler(trieDB)
			require.NoError(t, net.AddHandler(p2p.EVMLeafRequestHandlerID, handler))

			target := rawdb.NewMemoryDatabase()
			syncer, err := NewSyncer(NewClient(net, tracker), target, root, common.Hash{})
			require.NoError(t, err)
			require.NoError(t, syncer.Sync(ctx))

			require.Equal(t, tt.wantRequests, requests.Load())
			requireReconstructed(t, target, root, keys, vals)
		})
	}
}

func TestNewSyncer_Validation(t *testing.T) {
	_, err := NewSyncer(nil, rawdb.NewMemoryDatabase(), common.Hash{}, common.Hash{})
	require.ErrorIs(t, err, errRootRequired)
}

func TestSyncer_ContextCancelled(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 10)

	ctx, cancel := context.WithCancel(t.Context())
	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, RegisterHandler(net, logging.NoLog{}, trieDB, common.HashLength, nil))

	syncer, err := NewSyncer(NewClient(net, tracker), rawdb.NewMemoryDatabase(), root, common.Hash{})
	require.NoError(t, err)

	cancel() // cancel before Sync runs
	require.ErrorIs(t, syncer.Sync(ctx), context.Canceled)
}

func TestSyncer_RejectsTamperedResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	nodeID := ids.GenerateTestNodeID()

	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 50)

	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	// Every response is tampered. Cancel after a few retries, no wall-clock wait.
	tampering := flakyLeafHandler(trieDB, -1)
	var attempts atomic.Int32
	handler := p2p.TestHandler{
		AppRequestF: func(c context.Context, n ids.NodeID, d time.Time, b []byte) ([]byte, *avacommon.AppError) {
			if attempts.Add(1) >= 3 {
				cancel()
			}
			return tampering.AppRequest(c, n, d, b)
		},
	}
	require.NoError(t, net.AddHandler(p2p.EVMLeafRequestHandlerID, handler))

	target := rawdb.NewMemoryDatabase()
	syncer, err := NewSyncer(NewClient(net, tracker), target, root, common.Hash{})
	require.NoError(t, err)
	require.ErrorIs(t, syncer.Sync(ctx), context.Canceled, "tampered leaves must never be accepted")

	// Nothing accepted, target stays empty.
	it := target.NewIterator(nil, nil)
	defer it.Release()
	require.False(t, it.Next(), "tampered responses must not write to the target")
}

func TestSyncer_RecoversAfterBadResponses(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	nodeID := ids.GenerateTestNodeID()

	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, 50)

	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	// Corrupt the first two responses, then serve correctly.
	require.NoError(t, net.AddHandler(p2p.EVMLeafRequestHandlerID, flakyLeafHandler(trieDB, 2)))

	target := rawdb.NewMemoryDatabase()
	syncer, err := NewSyncer(NewClient(net, tracker), target, root, common.Hash{})
	require.NoError(t, err)
	require.NoError(t, syncer.Sync(ctx), "the re-request loop must recover after transient bad responses")

	requireReconstructed(t, target, root, keys, vals)
}

// requireReconstructed asserts every pair is queryable through the trie rebuilt
// into target at root.
func requireReconstructed(t *testing.T, target ethdb.Database, root common.Hash, keys, vals [][]byte) {
	t.Helper()
	tr, err := trie.New(trie.TrieID(root), triedb.NewDatabase(target, nil))
	require.NoError(t, err)
	for i, k := range keys {
		got, err := tr.Get(k)
		require.NoError(t, err)
		require.Equal(t, vals[i], got)
	}
}

// countingLeafHandler serves leaves and counts how many requests it receives,
// so a test can assert the syncer's batching.
func countingLeafHandler(trieDB *triedb.Database) (p2p.Handler, *atomic.Int32) {
	inner := handlers.NewHandler(
		logging.NoLog{},
		func() *syncpb.GetLeafRequest { return &syncpb.GetLeafRequest{} },
		newResponder(trieDB, common.HashLength, nil),
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

// flakyLeafHandler corrupts a value in the first badResponses responses so their
// range proof fails, then serves correctly. A negative badResponses corrupts
// every response.
func flakyLeafHandler(trieDB *triedb.Database, badResponses int32) p2p.Handler {
	inner := newResponder(trieDB, common.HashLength, nil)
	var count atomic.Int32
	return p2p.TestHandler{
		AppRequestF: func(ctx context.Context, n ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *avacommon.AppError) {
			req := &syncpb.GetLeafRequest{}
			if err := proto.Unmarshal(requestBytes, req); err != nil {
				return nil, avacommon.ErrUndefined
			}
			resp, err := inner.Respond(ctx, n, req)
			if err != nil || resp == nil || len(resp.Values) == 0 {
				return nil, avacommon.ErrUndefined
			}
			if seen := count.Add(1); badResponses < 0 || seen <= badResponses {
				resp.Values[0] = bytes.Repeat([]byte{0xff}, common.HashLength)
			}
			respBytes, err := proto.Marshal(resp)
			if err != nil {
				return nil, avacommon.ErrUndefined
			}
			return respBytes, nil
		},
	}
}
