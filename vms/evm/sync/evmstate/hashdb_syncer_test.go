// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evmstate

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// A sync leaves goroutines behind if teardown breaks.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}

type (
	sutConfig struct {
		codeDB      ethdb.Database
		target      ethdb.Database
		threshold   uint64
		leafHandler p2p.Handler
	}
	sutOption = options.Option[sutConfig]
)

// withCodeDB serves contract code out of db.
func withCodeDB(db ethdb.Database) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.codeDB = db
	})
}

// withTarget resumes onto an existing target instead of a fresh one.
func withTarget(db ethdb.Database) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.target = db
	})
}

// withThreshold overrides the split threshold. 1 forces every trie to segment.
func withThreshold(n uint64) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.threshold = n
	})
}

// withLeafHandler replaces the leaf handler, to count or tamper with responses.
func withLeafHandler(h p2p.Handler) sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		c.leafHandler = h
	})
}

// SUT is the system under test: both syncers wired to a loopback network. Prefer
// [SUT.sync] and [SUT.Target] over reaching into the fields.
type SUT struct {
	state  *HashDBSyncer
	code   *code.Syncer
	queue  *code.Queue
	target ethdb.Database
}

// newSUT serves the trie at root on a loopback network.
func newSUT(t *testing.T, ctx context.Context, trieDB *triedb.Database, root common.Hash, opts ...sutOption) *SUT {
	t.Helper()

	cfg := options.ApplyTo(&sutConfig{
		codeDB: rawdb.NewMemoryDatabase(),
		target: rawdb.NewMemoryDatabase(),
	}, opts...)

	net, tracker := synctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	if cfg.leafHandler != nil {
		require.NoError(t, net.AddHandler(p2p.EVMLeafRequestHandlerID, cfg.leafHandler))
	} else {
		require.NoError(t, RegisterHandler(logging.NoLog{}, net, trieDB, common.HashLength, nil))
	}
	require.NoError(t, code.RegisterHandler(logging.NoLog{}, net, cfg.codeDB))

	queue, err := code.NewQueue(cfg.target)
	require.NoError(t, err)

	state, err := NewHashDBSyncer(logging.NoLog{}, NewClient(net, tracker), cfg.target, root, queue)
	require.NoError(t, err)
	if cfg.threshold > 0 {
		state.threshold = cfg.threshold
	}

	return &SUT{
		state:  state,
		code:   code.NewSyncer(logging.NoLog{}, code.NewClient(net, tracker), cfg.target, queue.CodeHashes()),
		queue:  queue,
		target: cfg.target,
	}
}

// Target returns the database the syncers reconstruct state into.
func (s *SUT) Target() ethdb.Database { return s.target }

// sync runs both syncers, finalizes so a later run can resume, and asserts teardown.
func (s *SUT) sync(t *testing.T, ctx context.Context) error {
	t.Helper()

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error { return s.code.Sync(egCtx) })
	eg.Go(func() error { return s.state.Sync(egCtx) })
	syncErr := eg.Wait()

	// Flush in-progress writes so the next run can resume. No-op on success.
	require.NoError(t, s.state.Finalize())
	requireQueueClosed(t, s.queue)

	return syncErr
}

// cancelAfterN cancels once the n-th request arrives. A non-positive n never cancels.
func cancelAfterN(inner p2p.Handler, n int32, cancel context.CancelFunc) (p2p.Handler, *atomic.Int32) {
	var requests atomic.Int32
	h := p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, b []byte) ([]byte, *avacommon.AppError) {
			if got := requests.Add(1); n > 0 && got >= n {
				cancel()
			}
			return inner.AppRequest(ctx, nodeID, deadline, b)
		},
	}
	return h, &requests
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			f := synctest.NewStateFixture(t, tt.accounts)
			require.Len(t, f.Storage, tt.wantStorageTries)

			sut := newSUT(t, ctx, f.TrieDB, f.Root, withCodeDB(f.CodeDB))
			require.NoError(t, sut.sync(t, ctx))
			target := sut.Target()

			requireReconstructed(t, target, f.Root, f.AccKeys, f.AccVals)
			requireAccountSnapshots(t, target, f.AccKeys)
			requireCode(t, target, f.Codes)
			requireStorageReconstructed(t, target, f.Storage)
		})
	}
}

// Segmentation through the orchestrator. TestStateTrie_SegmentedStorageReconstruct
// only drives stateTrie directly.
func TestHashDBSyncer_SegmentsStorageTrie(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Big enough to span several requests, so the first segment triggers a split.
	f := synctest.NewStateFixture(t, []synctest.AccountDesc{{StorageSize: 3000}, {}})
	require.Len(t, f.Storage, 1)

	var storageRoot common.Hash
	for root := range f.Storage {
		storageRoot = root
	}

	handler, starts := recordLeafStarts(leafHandlerFor(f.TrieDB), storageRoot)
	sut := newSUT(t, ctx, f.TrieDB, f.Root,
		withCodeDB(f.CodeDB),
		withLeafHandler(handler),
		withThreshold(1), // force the storage trie to segment
	)
	require.NoError(t, sut.sync(t, ctx))

	// Only segments 1..n-1 start on an exact boundary, which a left-to-right walk
	// never requests.
	boundaries := set.NewSet[string](numStorageTrieSegments - 1)
	for i := 1; i < numStorageTrieSegments; i++ {
		start, _ := segmentRange(i, numStorageTrieSegments)
		boundaries.Add(string(start))
	}

	var onBoundary int
	for _, start := range starts() {
		if boundaries.Contains(string(start)) {
			onBoundary++
		}
	}
	require.Positive(t, onBoundary, "the storage trie must have been fetched in segments")

	requireStorageReconstructed(t, sut.Target(), f.Storage)
}

// recordLeafStarts records the StartKey of every leaf request for root.
func recordLeafStarts(inner p2p.Handler, root common.Hash) (p2p.Handler, func() [][]byte) {
	var (
		lock   sync.Mutex
		starts [][]byte
	)
	h := p2p.TestHandler{
		AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, b []byte) ([]byte, *avacommon.AppError) {
			req := &syncpb.GetLeafRequest{}
			if err := proto.Unmarshal(b, req); err == nil && bytes.Equal(req.GetRootHash(), root.Bytes()) {
				lock.Lock()
				starts = append(starts, req.GetStartKey())
				lock.Unlock()
			}
			return inner.AppRequest(ctx, nodeID, deadline, b)
		},
	}
	return h, func() [][]byte {
		lock.Lock()
		defer lock.Unlock()
		return slices.Clone(starts)
	}
}

// Another root's leaves in the snapshot must fail the root check, and pass once
// wiped. See the precondition on [NewHashDBSyncer].
func TestHashDBSyncer_RejectsStaleSnapshot(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	trieDB := synctest.NewTrieDB()
	staleRoot, _, _ := synctest.FillAccountTrieDistributed(t, trieDB, 200)
	root, keys, vals := synctest.FillAccountTrieDistributed(t, trieDB, 150)
	require.NotEqual(t, staleRoot, root)

	target := rawdb.NewMemoryDatabase()
	require.NoError(t, newSUT(t, ctx, trieDB, staleRoot, withTarget(target)).sync(t, ctx))

	// The previous root's leaves count as resume progress.
	err := newSUT(t, ctx, trieDB, root, withTarget(target)).sync(t, ctx)
	require.ErrorIs(t, err, errRootMismatch)

	// Wiping the snapshot is the caller's job, and it clears the stale progress.
	wipeAccountSnapshot(t, target)
	require.NoError(t, newSUT(t, ctx, trieDB, root, withTarget(target)).sync(t, ctx))
	requireReconstructed(t, target, root, keys, vals)
}

// wipeAccountSnapshot stands in for the engine-side wipe.
func wipeAccountSnapshot(t *testing.T, db ethdb.Database) {
	t.Helper()
	it := db.NewIterator(rawdb.SnapshotAccountPrefix, nil)
	defer it.Release()

	batch := db.NewBatch()
	for it.Next() {
		require.NoError(t, batch.Delete(common.CopyBytes(it.Key())))
	}
	require.NoError(t, it.Error())
	require.NoError(t, batch.Write())
}

// More tries than the scheduler has slots, so the producer must wait for slots to
// come back. No other test exceeds that limit.
func TestHashDBSyncer_RecyclesTrieSlots(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	// Distinct storage sizes give every account its own trie root.
	const numTries = 3 * defaultLeafWorkers
	descs := make([]synctest.AccountDesc, numTries)
	for i := range descs {
		descs[i] = synctest.AccountDesc{StorageSize: i + 2}
	}

	f := synctest.NewStateFixture(t, descs)
	require.Greater(t, len(f.Storage), defaultLeafWorkers, "the run must exceed the scheduler's slots")

	sut := newSUT(t, ctx, f.TrieDB, f.Root, withCodeDB(f.CodeDB))
	require.NoError(t, sut.sync(t, ctx))
	requireStorageReconstructed(t, sut.Target(), f.Storage)
}

// A held slot would stall the scheduler's close barrier, hanging the sync.
func TestHashDBSyncer_StorageTrieDoneReturnsSlot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// failClear makes the marker clear fail, so the callback errors before releasing.
		failClear bool
	}{
		{
			name: "clean completion",
		},
		{
			name:      "failed marker clear",
			failClear: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := rawdb.NewMemoryDatabase()
			if tt.failClear {
				db = failingBatchDB{db}
			}

			root := common.HexToHash("0xaa")
			scheduler := newTrieScheduler(1, 1)
			require.NoError(t, scheduler.queueStorage(t.Context(), root, &stateTrie{}))
			require.Empty(t, scheduler.slots, "the trie holds the only slot")

			s := &HashDBSyncer{
				scheduler: scheduler,
				stats:     newTrieSyncStats(logging.NoLog{}),
				trieQueue: newTrieQueue(db),
			}

			err := s.storageTrieDone(root)(t.Context())
			if tt.failClear {
				require.ErrorIs(t, err, errMarkerClearFailed)
			} else {
				require.NoError(t, err)
			}

			require.Len(t, scheduler.slots, 1, "the slot must come back")
			require.Empty(t, scheduler.tries, "the trie must stop being tracked")
		})
	}
}

var errMarkerClearFailed = errors.New("marker clear failed")

// failingBatchDB fails the batch write that clears a trie's markers.
type failingBatchDB struct {
	ethdb.Database
}

func (db failingBatchDB) NewBatch() ethdb.Batch {
	return failingBatch{db.Database.NewBatch()}
}

type failingBatch struct {
	ethdb.Batch
}

func (failingBatch) Write() error { return errMarkerClearFailed }

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
		{
			name:    "zero root",
			root:    common.Hash{},
			queue:   queue,
			wantErr: errRootRequired,
		},
		{
			name:    "nil queue",
			root:    common.HexToHash("0x1"),
			queue:   nil,
			wantErr: errCodeQueueRequired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHashDBSyncer(logging.NoLog{}, nil, db, tt.root, tt.queue)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// Sync builds the state Finalize walks, so calling it first must find nothing rather
// than panic on the unset fields.
func TestHashDBSyncer_FinalizeBeforeSync(t *testing.T) {
	t.Parallel()
	db := rawdb.NewMemoryDatabase()
	queue, err := code.NewQueue(db)
	require.NoError(t, err)
	defer queue.Shutdown()

	s, err := NewHashDBSyncer(logging.NoLog{}, nil, db, common.HexToHash("0xabc"), queue)
	require.NoError(t, err)
	require.NoError(t, s.Finalize())
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

// A never-converging sync must return the ctx error and tear down the code queue.
func TestHashDBSyncer_CancelPropagates(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	f := synctest.NewStateFixture(t, []synctest.AccountDesc{{WithCode: true}, {WithCode: true}})

	// Every response tampered, so only cancellation can end it.
	handler, _ := cancelAfterN(flakyLeafHandler(f.TrieDB, -1), 5, cancel)

	sut := newSUT(t, ctx, f.TrieDB, f.Root, withCodeDB(f.CodeDB), withLeafHandler(handler))
	require.ErrorIs(t, sut.sync(t, ctx), context.Canceled)
}

// Resume must fetch less than a fresh sync.
func TestHashDBSyncer_ResumesAfterInterrupt(t *testing.T) {
	t.Parallel()
	trieDB := synctest.NewTrieDB()
	root, keys, vals := synctest.FillAccountTrieDistributed(t, trieDB, 8000)

	// Baseline. Segmentation is forced on, so this also covers a split account trie.
	fresh := rawdb.NewMemoryDatabase()
	fullReqs, err := runResumableSync(t, trieDB, root, fresh, -1)
	require.NoError(t, err)
	requireReconstructed(t, fresh, root, keys, vals)
	requireAccountSnapshots(t, fresh, keys)
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

// runResumableSync syncs into target with segmentation forced on, cancelling after
// cancelAfter requests when positive.
func runResumableSync(t *testing.T, trieDB *triedb.Database, root common.Hash, target ethdb.Database, cancelAfter int32) (int32, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	handler, requests := cancelAfterN(leafHandlerFor(trieDB), cancelAfter, cancel)
	sut := newSUT(t, ctx, trieDB, root,
		withTarget(target),
		withThreshold(1), // force segmentation
		withLeafHandler(handler),
	)

	syncErr := sut.sync(t, ctx)
	return requests.Load(), syncErr
}

// leafHandlerFor returns the production leaf handler, for tests that wrap it.
func leafHandlerFor(trieDB *triedb.Database) p2p.Handler {
	return handlers.NewHandler[syncpb.GetLeafRequest](
		logging.NoLog{},
		newResponder(logging.NoLog{}, trieDB, common.HashLength, nil),
	)
}
