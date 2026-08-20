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
	r := newResponder(logging.NoLog{}, trieDB, common.HashLength)

	// A fixture. TestResponder_Serves owns the partial-proof rule.
	partial, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{RootHash: root.Bytes(), KeyLimit: 20})
	require.Nil(t, appErr)

	whole, appErr := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetLeafRequest{RootHash: root.Bytes(), KeyLimit: 50})
	require.Nil(t, appErr)

	tampered := proto.Clone(partial).(*syncpb.GetLeafResponse)
	tampered.Values[0] = bytes.Repeat([]byte{0xff}, common.HashLength)

	tests := []struct {
		name     string
		resp     *syncpb.GetLeafResponse
		wantMore bool
		wantErr  error
	}{
		{
			name:     "partial has more",
			resp:     partial,
			wantMore: true,
		},
		{
			name: "whole has no more",
			resp: whole,
		},
		{
			name:    "tampered value fails the proof",
			resp:    tampered,
			wantErr: errInvalidRangeProof,
		},
		{
			name:    "empty without proof",
			resp:    &syncpb.GetLeafResponse{},
			wantErr: errEmptyLeafResponse,
		},
		{
			name:    "too many leaves",
			resp:    &syncpb.GetLeafResponse{Keys: make([][]byte, MaxLeavesLimit+1)},
			wantErr: errTooManyLeaves,
		},
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

// recordingTask records the leaves handed to it.
type recordingTask struct {
	root     common.Hash
	keys     [][]byte
	finished int
}

func (r *recordingTask) Root() common.Hash  { return r.root }
func (*recordingTask) Account() common.Hash { return common.Hash{} }
func (*recordingTask) Start() []byte        { return nil }
func (*recordingTask) End() []byte          { return nil }

func (r *recordingTask) OnLeaves(_ context.Context, batch leafBatch) error {
	r.keys = append(r.keys, batch.keys...)
	return nil
}

func (r *recordingTask) OnFinish(context.Context) error {
	r.finished++
	return nil
}

// runLeafTask drives one task through a single worker.
func runLeafTask(t *testing.T, ctx context.Context, handler p2p.Handler, tk task) error {
	t.Helper()
	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, net.AddHandler(p2p.EVMLeafRequestHandlerID, handler))

	tasks := make(chan task, 1)
	tasks <- tk
	close(tasks)
	return newLeafFetcher(logging.NoLog{}, NewClient(net, tracker), tasks, 1).sync(ctx)
}

func TestLeafFetch_Batching(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		numKeys      int
		wantRequests int32
	}{
		{
			name:         "single batch",
			numKeys:      50,
			wantRequests: 1,
		},
		{
			name:         "exact limit",
			numKeys:      int(MaxLeavesLimit),
			wantRequests: 1,
		},
		{
			name:         "multiple batches",
			numKeys:      int(MaxLeavesLimit) + 50,
			wantRequests: 2,
		},
		{
			name:         "many batches",
			numKeys:      5000,
			wantRequests: 5,
		},
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

// The re-request loop recovers from transient bad responses but never accepts a
// tampered one, so a peer that always tampers can only be ended by cancellation.
func TestLeafFetch_BadResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// badResponses is how many responses to corrupt, negative for every one.
		badResponses int32
		cancelAfter  int32
		wantErr      error
	}{
		{
			name:         "recovers after two bad responses",
			badResponses: 2,
		},
		{
			name:         "never accepts tampered leaves",
			badResponses: -1,
			cancelAfter:  3,
			wantErr:      context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			trieDB := synctest.NewTrieDB()
			root, keys, _ := synctest.FillTrie(t, trieDB, 50)
			handler, _ := cancelAfterN(flakyLeafHandler(trieDB, tt.badResponses), tt.cancelAfter, cancel)

			tk := &recordingTask{root: root}
			err := runLeafTask(t, ctx, handler, tk)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Empty(t, tk.keys, "tampered leaves must not reach the task")
				return
			}
			require.NoError(t, err)
			require.Equal(t, keys, tk.keys)
		})
	}
}

// requireReconstructed reads every pair back through the rebuilt trie.
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

// countingLeafHandler counts the requests it serves.
func countingLeafHandler(trieDB *triedb.Database) (p2p.Handler, *atomic.Int32) {
	inner := handlers.NewHandler[syncpb.GetLeafRequest](
		logging.NoLog{},
		newResponder(logging.NoLog{}, trieDB, common.HashLength),
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

// flakyLeafHandler corrupts the first badResponses responses. Negative corrupts all.
func flakyLeafHandler(trieDB *triedb.Database, badResponses int32) p2p.Handler {
	inner := newResponder(logging.NoLog{}, trieDB, common.HashLength)
	var count atomic.Int32
	return p2p.TestHandler{
		AppRequestF: func(ctx context.Context, n ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *avacommon.AppError) {
			req := &syncpb.GetLeafRequest{}
			if err := proto.Unmarshal(requestBytes, req); err != nil {
				return nil, avacommon.ErrUndefined
			}
			resp, appErr := inner.Respond(ctx, n, req)
			if appErr != nil || resp == nil || len(resp.Values) == 0 {
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
