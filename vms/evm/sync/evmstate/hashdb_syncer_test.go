// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// runStateSync runs the state and code syncers concurrently against a loopback network.
func runStateSync(t *testing.T, ctx context.Context, f *synctest.StateFixture) ethdb.Database {
	t.Helper()

	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, RegisterHandler(net, logging.NoLog{}, f.TrieDB, common.HashLength, nil))
	require.NoError(t, code.RegisterHandler(net, logging.NoLog{}, f.CodeDB))

	target := rawdb.NewMemoryDatabase()

	queue, err := code.NewQueue(target)
	require.NoError(t, err)
	codeSyncer := code.NewSyncer(code.NewClient(net, tracker), target, queue.CodeHashes())

	stateSyncer, err := NewHashDBSyncer(logging.NoLog{}, NewClient(net, tracker), target, f.Root, queue)
	require.NoError(t, err)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return stateSyncer.Sync(egCtx) })
	require.NoError(t, eg.Wait())

	return target
}

func TestHashDBSyncer_Reconstruction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		accounts         []synctest.AccountDesc
		wantStorageTries int
	}{
		{
			name:     "accounts with code",
			accounts: []synctest.AccountDesc{{WithCode: true}, {}, {WithCode: true}, {WithCode: true}, {}},
		},
		{
			name:             "shared storage roots",
			accounts:         []synctest.AccountDesc{{StorageSize: 5}, {StorageSize: 6, WithCode: true}, {StorageSize: 5}, {WithCode: true}, {}},
			wantStorageTries: 2,
		},
		{
			name: "many concurrent storage tries",
			accounts: []synctest.AccountDesc{
				{StorageSize: 5},
				{StorageSize: 6},
				{StorageSize: 7},
				{StorageSize: 8},
				{StorageSize: 9, WithCode: true},
				{StorageSize: 10},
				{StorageSize: 11},
				{StorageSize: 12},
			},
			wantStorageTries: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			f := synctest.NewStateFixture(t, tt.accounts)
			require.Len(t, f.Storage, tt.wantStorageTries)
			target := runStateSync(t, ctx, f)

			requireReconstructed(t, target, f.Root, f.AccKeys, f.AccVals)
			requireAccountSnapshots(t, target, f.AccKeys)
			requireCode(t, target, f.Codes)
			requireStorageReconstructed(t, target, f.Storage)
		})
	}
}

// TestHashDBSyncer_SegmentsLargeAccountTrie asserts a split account trie's concurrent segments feed one trie in order.
func TestHashDBSyncer_SegmentsLargeAccountTrie(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	trieDB := synctest.NewTrieDB()
	root, keys, vals := fillDistributedAccountTrie(t, trieDB, 4000)

	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, RegisterHandler(net, logging.NoLog{}, trieDB, common.HashLength, nil))
	require.NoError(t, code.RegisterHandler(net, logging.NoLog{}, rawdb.NewMemoryDatabase()))

	target := rawdb.NewMemoryDatabase()
	queue, err := code.NewQueue(target)
	require.NoError(t, err)
	codeSyncer := code.NewSyncer(code.NewClient(net, tracker), target, queue.CodeHashes())
	stateSyncer, err := NewHashDBSyncer(logging.NoLog{}, NewClient(net, tracker), target, root, queue)
	require.NoError(t, err)
	stateSyncer.threshold = 1 // force the account trie to segment

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return stateSyncer.Sync(egCtx) })
	require.NoError(t, eg.Wait())

	requireReconstructed(t, target, root, keys, vals)
	requireAccountSnapshots(t, target, keys)
}

func requireAccountSnapshots(t *testing.T, target ethdb.Database, accKeys [][]byte) {
	t.Helper()
	for _, k := range accKeys {
		require.NotNil(t, rawdb.ReadAccountSnapshot(target, common.BytesToHash(k)), "account snapshot for %x", k)
	}
}

func requireCode(t *testing.T, target ethdb.Database, codes map[common.Hash][]byte) {
	t.Helper()
	for hash, blob := range codes {
		require.Equal(t, blob, rawdb.ReadCode(target, hash), "code for %s", hash)
	}
}

// requireStorageReconstructed asserts every storage trie and its snapshots reconstruct into target.
func requireStorageReconstructed(t *testing.T, target ethdb.Database, storage map[common.Hash]*synctest.StorageFixture) {
	t.Helper()
	for storageRoot, st := range storage {
		requireReconstructed(t, target, storageRoot, st.Keys, st.Vals)
		for _, account := range st.Accounts {
			for i, k := range st.Keys {
				got := rawdb.ReadStorageSnapshot(target, account, common.BytesToHash(k))
				require.Equal(t, st.Vals[i], got, "storage snapshot account %s slot %x", account, k)
			}
		}
	}
}

func TestNewHashDBSyncer_Validation(t *testing.T) {
	t.Parallel()
	db := rawdb.NewMemoryDatabase()
	queue, err := code.NewQueue(db)
	require.NoError(t, err)
	defer queue.Shutdown()

	tests := []struct {
		name    string
		root    common.Hash
		queue   *code.Queue
		wantErr error
	}{
		{name: "zero root", root: common.Hash{}, queue: queue, wantErr: errRootRequired},
		{name: "nil queue", root: common.HexToHash("0x1"), queue: nil, wantErr: errCodeQueueRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHashDBSyncer(logging.NoLog{}, nil, db, tt.root, tt.queue)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestHashDBSyncer_FinalizeCodeQueueAbortsOnCancel verifies a full code queue with no
// consumer must not wedge finalize on ctx cancel.
func TestHashDBSyncer_FinalizeCodeQueueAbortsOnCancel(t *testing.T) {
	t.Parallel()
	db := rawdb.NewMemoryDatabase()
	queue, err := code.NewQueue(db, code.WithCapacity(1))
	require.NoError(t, err)

	// Fill past capacity with no consumer so the forwarder blocks.
	hashes := []common.Hash{common.HexToHash("0x1"), common.HexToHash("0x2"), common.HexToHash("0x3")}
	require.NoError(t, queue.AddCode(t.Context(), hashes))

	s, err := NewHashDBSyncer(logging.NoLog{}, nil, db, common.HexToHash("0xabc"), queue)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	done := make(chan error, 1)
	go func() { done <- s.finalizeCodeQueue(ctx) }()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("finalizeCodeQueue hung on a cancelled context with a full queue")
	}

	requireQueueClosed(t, queue)
}

// requireQueueClosed drains buffered hashes and asserts the channel is closed.
func requireQueueClosed(t *testing.T, queue *code.Queue) {
	t.Helper()
	for {
		select {
		case _, ok := <-queue.CodeHashes():
			if !ok {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("code queue channel was not closed")
		}
	}
}

// TestHashDBSyncer_CancelPropagates checks a never-converging sync
// returns the context error and tears down the code queue.
func TestHashDBSyncer_CancelPropagates(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	f := synctest.NewStateFixture(t, []synctest.AccountDesc{{WithCode: true}, {WithCode: true}})

	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	tampering := flakyLeafHandler(f.TrieDB, -1)
	var attempts atomic.Int32
	handler := p2p.TestHandler{
		AppRequestF: func(c context.Context, n ids.NodeID, d time.Time, b []byte) ([]byte, *avacommon.AppError) {
			if attempts.Add(1) >= 5 {
				cancel()
			}
			return tampering.AppRequest(c, n, d, b)
		},
	}
	require.NoError(t, net.AddHandler(p2p.EVMLeafRequestHandlerID, handler))
	require.NoError(t, code.RegisterHandler(net, logging.NoLog{}, f.CodeDB))

	target := rawdb.NewMemoryDatabase()
	queue, err := code.NewQueue(target)
	require.NoError(t, err)
	codeSyncer := code.NewSyncer(code.NewClient(net, tracker), target, queue.CodeHashes())
	stateSyncer, err := NewHashDBSyncer(logging.NoLog{}, NewClient(net, tracker), target, f.Root, queue)
	require.NoError(t, err)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return stateSyncer.Sync(egCtx) })
	require.ErrorIs(t, eg.Wait(), context.Canceled)

	requireQueueClosed(t, queue)
}

// TestHashDBSyncer_ResumesAfterInterrupt cancels a segmented sync partway,
// then asserts resume finishes with fewer leaf fetches than a fresh sync.
func TestHashDBSyncer_ResumesAfterInterrupt(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, keys, vals := fillDistributedAccountTrie(t, trieDB, 8000)

	// Baseline: a full sync from scratch.
	fresh := rawdb.NewMemoryDatabase()
	fullReqs, err := runResumableSync(t, trieDB, root, fresh, -1)
	require.NoError(t, err)
	requireReconstructed(t, fresh, root, keys, vals)
	require.Greater(t, fullReqs, int32(4), "the trie must take several requests to segment and sync")

	// Interrupt a fresh target partway through.
	target := rawdb.NewMemoryDatabase()
	_, err = runResumableSync(t, trieDB, root, target, fullReqs/2)
	require.ErrorIs(t, err, context.Canceled)

	// Resume on the same target.
	resumeReqs, err := runResumableSync(t, trieDB, root, target, -1)
	require.NoError(t, err)
	requireReconstructed(t, target, root, keys, vals)
	require.Positive(t, resumeReqs, "resume must still fetch the unsynced remainder")
	require.Less(t, resumeReqs, fullReqs, "resume must skip the persisted progress")
}

// runResumableSync syncs the account trie into target with segmentation forced on,
// cancelling after cancelAfter requests when positive. It finalizes and returns the request count and error.
func runResumableSync(t *testing.T, trieDB *triedb.Database, root common.Hash, target ethdb.Database, cancelAfter int32) (int32, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	inner := handlers.NewHandler(
		logging.NoLog{},
		func() *syncpb.GetLeafRequest { return &syncpb.GetLeafRequest{} },
		newResponder(trieDB, common.HashLength, nil),
	)
	var requests atomic.Int32
	handler := p2p.TestHandler{
		AppRequestF: func(c context.Context, nodeID ids.NodeID, d time.Time, b []byte) ([]byte, *avacommon.AppError) {
			if n := requests.Add(1); cancelAfter > 0 && n >= cancelAfter {
				cancel()
			}
			return inner.AppRequest(c, nodeID, d, b)
		},
	}
	require.NoError(t, net.AddHandler(p2p.EVMLeafRequestHandlerID, handler))
	require.NoError(t, code.RegisterHandler(net, logging.NoLog{}, rawdb.NewMemoryDatabase()))

	queue, err := code.NewQueue(target)
	require.NoError(t, err)
	codeSyncer := code.NewSyncer(code.NewClient(net, tracker), target, queue.CodeHashes())
	stateSyncer, err := NewHashDBSyncer(logging.NoLog{}, NewClient(net, tracker), target, root, queue)
	require.NoError(t, err)
	stateSyncer.threshold = 1 // force segmentation

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return codeSyncer.Sync(egCtx) })
	eg.Go(func() error { return stateSyncer.Sync(egCtx) })
	syncErr := eg.Wait()

	// Flush in-progress writes so the next run can resume. No-op on success.
	require.NoError(t, stateSyncer.Finalize())

	return requests.Load(), syncErr
}
