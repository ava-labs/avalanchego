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
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestVerifyLeafs(t *testing.T) {
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 50)
	r := newResponder(loggingtest.New(t, logging.Debug), trieDB, common.HashLength)

	partial, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{RootHash: root.Bytes(), KeyLimit: 20})
	require.Nil(t, appErr)
	require.NotEmpty(t, partial.ProofVals, "partial range must carry a proof")

	whole, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{RootHash: root.Bytes(), KeyLimit: 50})
	require.Nil(t, appErr)
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

			trieDB := synctest.NewTrieDB()
			root, keys, vals := synctest.FillTrie(t, trieDB, tt.numKeys)
			counting := &countingResponder{inner: newLeafResponder(t, trieDB)}
			syncer, target := newSyncer(t, ctx, root, leafHandler(t, counting))
			require.NoError(t, syncer.Sync(ctx))

			require.Equal(t, tt.wantRequests, counting.served.Load())
			requireReconstructed(t, target, root, keys, vals)
		})
	}
}

func TestNewSyncer_Validation(t *testing.T) {
	_, err := NewSyncer(loggingtest.New(t, logging.Debug), nil, rawdb.NewMemoryDatabase(), common.Hash{}, common.Hash{})
	require.ErrorIs(t, err, errRootRequired)
}

func TestSyncer_ContextCancelled(t *testing.T) {
	nodeID := ids.GenerateTestNodeID()
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 10)

	ctx, cancel := context.WithCancel(t.Context())
	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, RegisterHandler(loggingtest.New(t, logging.Debug), net, trieDB, common.HashLength))

	syncer, err := NewSyncer(loggingtest.New(t, logging.Debug), NewClient(net, tracker), rawdb.NewMemoryDatabase(), root, common.Hash{})
	require.NoError(t, err)

	cancel() // cancel before Sync runs
	require.ErrorIs(t, syncer.Sync(ctx), context.Canceled)
}

func TestSyncer_RejectsTamperedResponse(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 50)

	// Every response is tampered. Cancel after a few retries, no wall-clock wait.
	tampering := &flakyResponder{inner: newLeafResponder(t, trieDB), bad: -1}
	handler := leafHandler(t, &cancelAfter{inner: tampering, after: 3, cancel: cancel})
	syncer, target := newSyncer(t, ctx, root, handler)
	require.ErrorIs(t, syncer.Sync(ctx), context.Canceled, "tampered leaves must never be accepted")

	// Nothing accepted, target stays empty.
	it := target.NewIterator(nil, nil)
	defer it.Release()
	require.False(t, it.Next(), "tampered responses must not write to the target")
}

func TestSyncer_RecoversAfterBadResponses(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillTrie(t, trieDB, 50)

	// Corrupt the first two responses, then serve correctly.
	syncer, target := newSyncer(t, ctx, root,
		leafHandler(t, &flakyResponder{inner: newLeafResponder(t, trieDB), bad: 2}))
	require.NoError(t, syncer.Sync(ctx), "the re-request loop must recover after transient bad responses")

	requireReconstructed(t, target, root, keys, vals)
}

// newSyncer wires a loopback network serving handler and returns a syncer for the
// trie at root together with its target db.
func newSyncer(t *testing.T, ctx context.Context, root common.Hash, handler p2p.Handler) (*Syncer, ethdb.Database) {
	t.Helper()
	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, net.AddHandler(p2p.EVMLeafRequestHandlerID, handler))
	target := rawdb.NewMemoryDatabase()
	syncer, err := NewSyncer(loggingtest.New(t, logging.Debug), NewClient(net, tracker), target, root, common.Hash{})
	require.NoError(t, err)
	return syncer, target
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

// countingResponder counts the requests it serves, so a test can assert the
// syncer's batching.
type countingResponder struct {
	inner  *responder
	served atomic.Int32
}

func (c *countingResponder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetLeafRequest) (*syncpb.GetLeafResponse, *avacommon.AppError) {
	c.served.Add(1)
	return c.inner.Respond(ctx, nodeID, req)
}

// flakyResponder corrupts a value in the first bad responses so their range
// proof fails, then serves correctly. A negative bad corrupts every response.
type flakyResponder struct {
	inner  *responder
	bad    int32
	served atomic.Int32
}

func (f *flakyResponder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetLeafRequest) (*syncpb.GetLeafResponse, *avacommon.AppError) {
	resp, appErr := f.inner.Respond(ctx, nodeID, req)
	if appErr != nil || len(resp.GetValues()) == 0 {
		return resp, appErr
	}
	if seen := f.served.Add(1); f.bad < 0 || seen <= f.bad {
		resp.Values[0] = bytes.Repeat([]byte{0xff}, common.HashLength)
	}
	return resp, nil
}

// leafHandler drives r through the real shell, so the tests exercise the
// production unmarshal, marshal and error path.
func leafHandler(tb testing.TB, r handlers.Responder[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]) p2p.Handler {
	tb.Helper()
	return handlers.NewHandler(loggingtest.New(tb, logging.Debug), r)
}

// cancelAfter cancels once it has served after requests, bounding a retry loop
// without a wall-clock wait.
type cancelAfter struct {
	inner  handlers.Responder[*syncpb.GetLeafRequest, *syncpb.GetLeafResponse]
	after  int32
	cancel context.CancelFunc
	served atomic.Int32
}

func (c *cancelAfter) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetLeafRequest) (*syncpb.GetLeafResponse, *avacommon.AppError) {
	if c.served.Add(1) >= c.after {
		c.cancel()
	}
	return c.inner.Respond(ctx, nodeID, req)
}

func newLeafResponder(tb testing.TB, trieDB *triedb.Database) *responder {
	tb.Helper()
	return newResponder(loggingtest.New(tb, logging.Debug), trieDB, common.HashLength)
}
