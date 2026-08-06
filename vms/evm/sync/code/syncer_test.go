// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"crypto/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestVerifyCode(t *testing.T) {
	code := []byte("contract bytecode")
	hash := crypto.Keccak256Hash(code)

	oversized := make([]byte, params.MaxCodeSize+1)
	oversizedHash := crypto.Keccak256Hash(oversized)

	tests := []struct {
		name    string
		hashes  []common.Hash
		data    [][]byte
		wantErr error
	}{
		{
			name:   "valid",
			hashes: []common.Hash{hash},
			data:   [][]byte{code},
		},
		{
			name:    "count_mismatch",
			hashes:  []common.Hash{hash},
			data:    [][]byte{},
			wantErr: errCodeCountMismatch,
		},
		{
			name:    "hash_mismatch",
			hashes:  []common.Hash{hash},
			data:    [][]byte{[]byte("tampered")},
			wantErr: errCodeHashMismatch,
		},
		{
			name:    "size_exceeded",
			hashes:  []common.Hash{oversizedHash},
			data:    [][]byte{oversized},
			wantErr: errCodeSizeExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, verifyCode(tt.hashes, tt.data), tt.wantErr)
		})
	}
}

func TestSyncer(t *testing.T) {
	tests := []struct {
		name          string
		numFromSource int
		numOnDisk     int
		perReq        int
		wantRequests  int
	}{
		{name: "single_blob", numFromSource: 1, wantRequests: 1},
		{name: "batches_across_requests", numFromSource: 40, perReq: 4, wantRequests: 10},
		{name: "skips_code_already_on_disk", numFromSource: 3, numOnDisk: 2, wantRequests: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A broken skip re-requests forever, so bound the wait.
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			source := memorydb.New()
			target := memorydb.New()
			want := map[common.Hash][]byte{}

			for range tt.numFromSource {
				code := randomCode(t)
				want[writeCode(t, source, code)] = code
			}
			// Only in the target, so skipping must avoid requesting them.
			for range tt.numOnDisk {
				code := randomCode(t)
				want[writeCode(t, target, code)] = code
			}

			log := loggingtest.New(t, logging.Debug)
			counter := &countingResponder{inner: newResponder(log, source)}
			client := serve(t, ctx, log, counter)

			ch := make(chan common.Hash, len(want))
			for hash := range want {
				require.NoError(t, customrawdb.WriteCodeToFetch(target, hash))
				ch <- hash
			}
			close(ch)

			s := NewSyncer(log, client, target, ch)
			if tt.perReq > 0 {
				s.codeHashesPerReq = tt.perReq
			}
			require.NoError(t, s.Sync(ctx))

			for hash, code := range want {
				require.Equal(t, code, rawdb.ReadCode(target, hash))
			}

			it := customrawdb.NewCodeToFetchIterator(target)
			defer it.Release()
			require.False(t, it.Next(), "all to-fetch markers must be cleared")

			sizes := counter.requests()
			require.Len(t, sizes, tt.wantRequests,
				"code already on disk must not be requested, and a full batch must be sent as its own request")
			requested := 0
			for _, size := range sizes {
				// The handler drops a request over the cap, so an overgrown
				// batch costs the whole request, not just the excess.
				require.LessOrEqual(t, size, s.codeHashesPerReq, "a request outgrew the batch size")
				requested += size
			}
			require.Equal(t, tt.numFromSource, requested, "every missing hash is requested once")
		})
	}
}

// A batch is only handed off once it holds something, so a non-positive size
// cannot turn the manager into a spin of empty requests.
func TestSyncer_NeverSendsEmptyRequest(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	log := loggingtest.New(t, logging.Debug)
	counter := &countingResponder{inner: newResponder(log, memorydb.New())}
	client := serve(t, ctx, log, counter)

	// Open and never fed, so the manager has nothing to batch.
	ch := make(chan common.Hash)

	s := NewSyncer(log, client, memorydb.New(), ch)
	s.codeHashesPerReq = 0

	require.ErrorIs(t, s.Sync(ctx), context.DeadlineExceeded)
	require.Empty(t, counter.requests(), "an empty batch must never be sent")
}

func TestSyncer_RejectsTamperedResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	hash := crypto.Keccak256Hash([]byte("real code"))

	log := loggingtest.New(t, logging.Debug)
	responder := &tamperingResponder{}
	client := serve(t, ctx, log, responder)

	got, err := getCode(ctx, log, client, []common.Hash{hash})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, got, "tampered code must never be accepted")
	require.Positive(t, responder.served.Load(), "the deadline must not expire before a tampered response is rejected")
}

// serve registers r on a single-node in-process network and returns a client
// bound to it.
func serve(t *testing.T, ctx context.Context, log logging.Logger, r handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]) *Client {
	t.Helper()

	nodeID := ids.GenerateTestNodeID()
	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, net.AddHandler(p2p.EVMCodeRequestHandlerID, handlers.NewHandler(log, r)))
	return NewClient(net, tracker)
}

// countingResponder records the hash count of every request reaching inner, so
// a test can assert how many round trips the syncer made and how big they were.
type countingResponder struct {
	inner handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]
	mu    sync.Mutex
	sizes []int
}

func (c *countingResponder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetCodeRequest) (*syncpb.GetCodeResponse, *avacommon.AppError) {
	c.mu.Lock()
	c.sizes = append(c.sizes, len(req.GetHashes()))
	c.mu.Unlock()
	return c.inner.Respond(ctx, nodeID, req)
}

func (c *countingResponder) requests() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.sizes...)
}

// tamperingResponder returns well-formed but wrong code, so verification always
// fails. It counts its answers so a test can show the rejection really happened.
type tamperingResponder struct{ served atomic.Int64 }

func (r *tamperingResponder) Respond(_ context.Context, _ ids.NodeID, req *syncpb.GetCodeRequest) (*syncpb.GetCodeResponse, *avacommon.AppError) {
	r.served.Add(1)
	data := make([][]byte, len(req.GetHashes()))
	for i := range data {
		data[i] = []byte("tampered")
	}
	return &syncpb.GetCodeResponse{Data: data}, nil
}

func writeCode(t *testing.T, db ethdb.KeyValueWriter, code []byte) common.Hash {
	t.Helper()
	hash := crypto.Keccak256Hash(code)
	rawdb.WriteCode(db, hash, code)
	return hash
}

func randomCode(t *testing.T) []byte {
	t.Helper()
	code := make([]byte, 128)
	_, err := rand.Read(code)
	require.NoError(t, err)
	return code
}
