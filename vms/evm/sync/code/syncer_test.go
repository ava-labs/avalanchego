// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"crypto/rand"
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
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
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
			name:    "count mismatch",
			hashes:  []common.Hash{hash},
			data:    [][]byte{},
			wantErr: errCodeCountMismatch,
		},
		{
			name:    "hash mismatch",
			hashes:  []common.Hash{hash},
			data:    [][]byte{[]byte("tampered")},
			wantErr: errCodeHashMismatch,
		},
		{
			name:    "size exceeded",
			hashes:  []common.Hash{oversizedHash},
			data:    [][]byte{oversized},
			wantErr: errCodeSizeExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyCode(tt.hashes, tt.data)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSyncer(t *testing.T) {
	tests := []struct {
		name          string
		numFromSource int
		numOnDisk     int
		perReq        int
	}{
		{name: "single blob", numFromSource: 1},
		{name: "batches across requests", numFromSource: 12, perReq: 4},
		{name: "skips code already on disk", numFromSource: 3, numOnDisk: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A broken skip re-requests forever, so bound the wait.
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			nodeID := ids.GenerateTestNodeID()

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

			net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
			require.NoError(t, RegisterHandler(net, logging.NoLog{}, source))

			ch := make(chan common.Hash, len(want))
			for hash := range want {
				require.NoError(t, customrawdb.WriteCodeToFetch(target, hash))
				ch <- hash
			}
			close(ch)

			var opts []SyncerOption
			if tt.perReq > 0 {
				opts = append(opts, WithCodeHashesPerRequest(tt.perReq))
			}
			require.NoError(t, NewSyncer(NewClient(net, tracker), target, ch, opts...).Sync(ctx))

			for hash, code := range want {
				require.Equal(t, code, rawdb.ReadCode(target, hash))
			}

			it := customrawdb.NewCodeToFetchIterator(target)
			defer it.Release()
			require.False(t, it.Next(), "all to-fetch markers must be cleared")
		})
	}
}

// TestSyncer_DedupesInFlight checks the same code hash enqueued many times is
// fetched from the network at most once, exercising the in-flight dedup that
// exists because accounts sharing a code hash enqueue it repeatedly.
func TestSyncer_DedupesInFlight(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	nodeID := ids.GenerateTestNodeID()

	source := memorydb.New()
	target := memorydb.New()
	blob := randomCode(t)
	hash := writeCode(t, source, blob)

	inner := handlers.NewHandler(
		logging.NoLog{},
		func() *syncpb.GetCodeRequest { return &syncpb.GetCodeRequest{} },
		newResponder(source),
	)
	var requests atomic.Int32
	counting := p2p.TestHandler{
		AppRequestF: func(c context.Context, n ids.NodeID, d time.Time, b []byte) ([]byte, *avacommon.AppError) {
			requests.Add(1)
			return inner.AppRequest(c, n, d, b)
		},
	}

	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, net.AddHandler(p2p.EVMCodeRequestHandlerID, counting))

	require.NoError(t, customrawdb.WriteCodeToFetch(target, hash))
	ch := make(chan common.Hash, 20)
	for range 20 {
		ch <- hash
	}
	close(ch)

	require.NoError(t, NewSyncer(NewClient(net, tracker), target, ch, WithNumWorkers(4)).Sync(ctx))

	require.Equal(t, blob, rawdb.ReadCode(target, hash))
	require.Equal(t, int32(1), requests.Load(), "a repeated hash must be fetched exactly once")
}

func TestSyncer_RejectsTamperedResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	nodeID := ids.GenerateTestNodeID()

	hash := crypto.Keccak256Hash([]byte("real code"))

	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, net.AddHandler(p2p.EVMCodeRequestHandlerID, tamperingHandler()))

	got, err := getCode(ctx, NewClient(net, tracker), []common.Hash{hash})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, got, "tampered code must never be accepted")
}

// tamperingHandler returns well-formed but wrong code, so verification always fails.
func tamperingHandler() p2p.Handler {
	return p2p.TestHandler{
		AppRequestF: func(_ context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *avacommon.AppError) {
			req := &syncpb.GetCodeRequest{}
			if err := proto.Unmarshal(requestBytes, req); err != nil {
				return nil, avacommon.ErrUndefined
			}
			data := make([][]byte, len(req.GetHashes()))
			for i := range data {
				data[i] = []byte("tampered")
			}
			respBytes, err := proto.Marshal(&syncpb.GetCodeResponse{Data: data})
			if err != nil {
				return nil, avacommon.ErrUndefined
			}
			return respBytes, nil
		},
	}
}

// TestSyncer_CleansMarkerRewrittenMidCleanup regresses issue #5353: a to-fetch
// marker rewritten by a concurrent AddCode mid-cleanup must still be cleared by a
// sibling worker, which holds only because cleanup runs before the inFlight gate.
func TestSyncer_CleansMarkerRewrittenMidCleanup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	nodeID := ids.GenerateTestNodeID()

	codeBytes := randomCode(t)
	codeHash := crypto.Keccak256Hash(codeBytes)

	// probeHash is a synchronization barrier. Its code is on disk, so dequeueing
	// it does not affect codeHash's marker.
	probeBytes := randomCode(t)
	probeHash := crypto.Keccak256Hash(probeBytes)

	rawDB := rawdb.NewMemoryDatabase()
	clientDB := newBlockingBatchDB(rawDB)
	rawdb.WriteCode(rawDB, codeHash, codeBytes)
	require.NoError(t, customrawdb.WriteCodeToFetch(rawDB, codeHash))
	rawdb.WriteCode(rawDB, probeHash, probeBytes)

	source := memorydb.New()
	rawdb.WriteCode(source, codeHash, codeBytes)
	rawdb.WriteCode(source, probeHash, probeBytes)

	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, RegisterHandler(net, logging.NoLog{}, source))

	ch := make(chan common.Hash)
	codeSyncer := NewSyncer(NewClient(net, tracker), clientDB, ch, WithNumWorkers(2))

	syncErrCh := make(chan error, 1)
	go func() { syncErrCh <- codeSyncer.Sync(ctx) }()

	// A worker takes codeHash, commits the marker delete, then pauses inside the
	// wrapped Batch.Write.
	ch <- codeHash
	<-clientDB.blocked

	// Rewrite the marker (modelling a concurrent AddCode) and enqueue a
	// duplicate for a sibling worker.
	require.NoError(t, customrawdb.WriteCodeToFetch(rawDB, codeHash))
	ch <- codeHash

	// Barrier: this send returns only after the sibling has processed the
	// duplicate and is back at the receive, so its decision is committed before
	// we release the paused worker.
	ch <- probeHash

	close(clientDB.release)
	close(ch)
	require.NoError(t, <-syncErrCh)

	it := customrawdb.NewCodeToFetchIterator(rawDB)
	defer it.Release()
	require.False(t, it.Next(), "stale code-to-fetch marker remained after sync")
	require.NoError(t, it.Error())
}

// TestSyncer_DuplicateAddCodeNoMarkerLeak stresses the same invariant
// end-to-end: many producers hammer AddCode for one hash and after sync no
// to-fetch markers may remain.
func TestSyncer_DuplicateAddCodeNoMarkerLeak(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		preWrite bool
	}{
		{name: "code already on disk", preWrite: true},
		{name: "code fetched during sync", preWrite: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			nodeID := ids.GenerateTestNodeID()

			codeBytes := randomCode(t)
			codeHash := crypto.Keccak256Hash(codeBytes)

			clientDB := rawdb.NewMemoryDatabase()
			if tt.preWrite {
				rawdb.WriteCode(clientDB, codeHash, codeBytes)
			}

			source := memorydb.New()
			rawdb.WriteCode(source, codeHash, codeBytes)
			net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
			require.NoError(t, RegisterHandler(net, logging.NoLog{}, source))

			codeQueue, err := NewQueue(clientDB)
			require.NoError(t, err)

			const (
				numWorkers   = 8
				numProducers = 8
				iterations   = 10_000
			)
			codeSyncer := NewSyncer(NewClient(net, tracker), clientDB, codeQueue.CodeHashes(), WithNumWorkers(numWorkers))

			syncErrCh := make(chan error, 1)
			go func() { syncErrCh <- codeSyncer.Sync(ctx) }()

			var producers errgroup.Group
			for range numProducers {
				producers.Go(func() error {
					for range iterations {
						if err := codeQueue.AddCode(ctx, []common.Hash{codeHash}); err != nil {
							return err
						}
					}
					return nil
				})
			}
			require.NoError(t, producers.Wait())
			require.NoError(t, codeQueue.Finalize())
			require.NoError(t, <-syncErrCh)

			require.Equal(t, codeBytes, rawdb.ReadCode(clientDB, codeHash))

			it := customrawdb.NewCodeToFetchIterator(clientDB)
			defer it.Release()
			require.False(t, it.Next(), "stale code-to-fetch marker remained after sync")
			require.NoError(t, it.Error())
		})
	}
}

// blockingBatchDB pauses inside the first Batch.Write after the commit lands.
// Subsequent writes are not blocked.
type blockingBatchDB struct {
	ethdb.Database
	primed  atomic.Bool
	blocked chan struct{}
	release chan struct{}
}

func newBlockingBatchDB(inner ethdb.Database) *blockingBatchDB {
	return &blockingBatchDB{
		Database: inner,
		blocked:  make(chan struct{}),
		release:  make(chan struct{}),
	}
}

func (db *blockingBatchDB) NewBatch() ethdb.Batch {
	return &blockingBatch{Batch: db.Database.NewBatch(), db: db}
}

type blockingBatch struct {
	ethdb.Batch
	db *blockingBatchDB
}

func (b *blockingBatch) Write() error {
	err := b.Batch.Write()
	if b.db.primed.CompareAndSwap(false, true) {
		close(b.db.blocked)
		<-b.db.release
	}
	return err
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
