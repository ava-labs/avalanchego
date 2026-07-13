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

func TestVerifyLeaves(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			more, err := verifyLeaves(root, nil, tt.resp)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMore, more)
		})
	}
}

// recordingTask is a test [task] that records the verified leaves handed to it.
type recordingTask struct {
	root     common.Hash
	keys     [][]byte
	finished int
}

func (r *recordingTask) Root() common.Hash  { return r.root }
func (*recordingTask) Account() common.Hash { return common.Hash{} }
func (*recordingTask) Start() []byte        { return nil }
func (*recordingTask) End() []byte          { return nil }

func (r *recordingTask) OnLeaves(_ context.Context, keys, _ [][]byte) error {
	r.keys = append(r.keys, keys...)
	return nil
}

func (r *recordingTask) OnFinish(context.Context) error {
	r.finished++
	return nil
}

// runLeafTask drives one task through a single worker against a loopback handler.
func runLeafTask(t *testing.T, ctx context.Context, handler p2p.Handler, tk task) error {
	t.Helper()
	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, net.AddHandler(p2p.EVMLeafRequestHandlerID, handler))

	tasks := make(chan task, 1)
	tasks <- tk
	close(tasks)
	return newCallbackSyncer(NewClient(net, tracker), tasks, 1).sync(ctx)
}

func TestLeafFetch_Batching(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		numKeys      int
		wantRequests int32
	}{
		{name: "single batch", numKeys: 50, wantRequests: 1},
		{name: "exact limit", numKeys: int(MaxLeavesLimit), wantRequests: 1},
		{name: "multiple batches", numKeys: int(MaxLeavesLimit) + 50, wantRequests: 2},
		{name: "many batches", numKeys: 5000, wantRequests: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			trieDB := synctest.NewTrieDB()
			root, keys, _ := synctest.FillTrie(t, trieDB, tt.numKeys)
			handler, requests := countingLeafHandler(trieDB)

			tk := &recordingTask{root: root}
			require.NoError(t, runLeafTask(t, ctx, handler, tk))

			require.Equal(t, tt.wantRequests, requests.Load())
			require.Equal(t, keys, tk.keys, "every leaf must be fetched in key order")
			require.Equal(t, 1, tk.finished, "the task must finish exactly once")
		})
	}
}

func TestLeafFetch_ContextCancelled(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 10)
	handler, _ := countingLeafHandler(trieDB)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, runLeafTask(t, ctx, handler, &recordingTask{root: root}), context.Canceled)
}

func TestLeafFetch_RejectsTamperedResponse(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	trieDB := synctest.NewTrieDB()
	root, _, _ := synctest.FillTrie(t, trieDB, 50)

	// Every response tampered. Cancel after a few retries.
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

	tk := &recordingTask{root: root}
	require.ErrorIs(t, runLeafTask(t, ctx, handler, tk), context.Canceled, "tampered leaves must never be accepted")
	require.Empty(t, tk.keys, "tampered responses must not be handed to the task")
}

func TestLeafFetch_RecoversAfterBadResponses(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	trieDB := synctest.NewTrieDB()
	root, keys, _ := synctest.FillTrie(t, trieDB, 50)

	// Corrupt the first two responses, then serve correctly.
	tk := &recordingTask{root: root}
	require.NoError(t, runLeafTask(t, ctx, flakyLeafHandler(trieDB, 2), tk), "the re-request loop must recover")
	require.Equal(t, keys, tk.keys)
}

// requireReconstructed asserts every pair is queryable through the trie rebuilt into target.
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

// countingLeafHandler serves leaves and counts the requests it receives.
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

// flakyLeafHandler fails the range proof on the first badResponses responses, then
// serves correctly. A negative badResponses corrupts every response.
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
