// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
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
		copies        int // times each hash is enqueued, zero means once
	}{
		{name: "empty"},
		{name: "single_blob", numFromSource: 1},
		{name: "batches_across_requests", numFromSource: 3 * maxHashesPerRequest},
		{name: "partial_final_batch", numFromSource: 2*maxHashesPerRequest + 1},
		{name: "skips_code_already_on_disk", numFromSource: 3, numOnDisk: 2},
		// Shared bytecode puts the same hash on the queue many times.
		{name: "repeats_fetched_once", numFromSource: 1, copies: 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
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
			// Only the trailing batch can be short, so the count follows directly.
			wantRequests := (tt.numFromSource + maxHashesPerRequest - 1) / maxHashesPerRequest

			recorder := synctest.NewRecordingResponder(newResponder(log, source))
			// A broken skip re-requests forever, so stop the run at the first
			// request beyond what batching explains rather than waiting it out.
			guard := synctest.NewCancelAfter(recorder, wantRequests+1, cancel)
			client := serve(t, ctx, log, guard)

			copies := max(tt.copies, 1)
			ch := make(chan common.Hash, len(want)*copies)
			// TODO(#5652): marking and enqueueing belong together in the queue's
			// AddCode. Build the fixture through it once that lands.
			for hash := range want {
				require.NoError(t, customrawdb.WriteCodeToFetch(target, hash))
				for range copies {
					ch <- hash
				}
			}
			close(ch)

			err := NewSyncer(log, client, target, ch).Sync(ctx)
			require.False(t, guard.Fired(), "the syncer requested more than batching explains")
			require.NoError(t, err)

			for hash, code := range want {
				require.Equal(t, code, rawdb.ReadCode(target, hash))
			}

			it := customrawdb.NewCodeToFetchIterator(target)
			defer it.Release()
			require.False(t, it.Next(), "all to-fetch markers must be cleared")

			sizes := requestSizes(recorder)
			require.Len(t, sizes, wantRequests,
				"only hashes that are missing and not already claimed are requested, and a full batch is sent as its own request")
			requested := 0
			for _, size := range sizes {
				// The handler drops a request over the cap, so an overgrown
				// batch costs the whole request, not just the excess.
				require.LessOrEqual(t, size, maxHashesPerRequest, "a request outgrew the batch size")
				requested += size
			}
			require.Equal(t, tt.numFromSource, requested, "every missing hash is requested once")
		})
	}
}

func TestClaimSet(t *testing.T) {
	t.Parallel()

	// A distinct hash per claim, so the count must stay under what one byte holds.
	const claims = 100

	// The set must not grow with the hashes seen, only with what is outstanding.
	var c claimSet
	batch := make([]common.Hash, 0, claims)
	for i := range claims {
		codeHash := common.Hash{byte(i)}
		require.True(t, c.claim(codeHash))
		require.False(t, c.claim(codeHash), "a held hash cannot be claimed again")
		batch = append(batch, codeHash)
	}
	require.Equal(t, len(batch), held(&c))

	c.release(batch...)
	require.Zero(t, held(&c), "a released batch must leave nothing behind")
	require.True(t, c.claim(batch[0]), "a released hash can be claimed again")
}

// held counts the claims, which [sync.Map] does not report.
func held(c *claimSet) int {
	n := 0
	c.m.Range(func(_, _ any) bool {
		n++
		return true
	})
	return n
}

func TestSyncer_RejectsTamperedResponse(t *testing.T) {
	// Enough bad answers to show a rejection is not a one-off, then a good one so
	// the test ends on the syncer's own terms rather than on a deadline.
	const tampered = 2

	ctx := t.Context()
	log := loggingtest.New(t, logging.Debug)
	source := memorydb.New()
	code := randomCode(t)
	hash := writeCode(t, source, code)

	// Well-formed but the wrong code, so only the client's own verification can
	// reject it.
	responder := synctest.NewMutatingResponder(newResponder(log, source), tampered, func(resp *syncpb.GetCodeResponse) {
		for i := range resp.GetData() {
			resp.Data[i] = []byte("tampered")
		}
	})
	client := serve(t, ctx, log, responder)

	got, err := getCode(ctx, log, client, []common.Hash{hash})
	require.NoError(t, err)
	require.Equal(t, [][]byte{code}, got, "tampered code must never be accepted")
	require.Equal(t, tampered+1, responder.Served(), "every tampered response must cost a re-request")
}

// A repeat reaching the batcher while its first copy is mid-commit must cost
// neither a second fetch nor a leaked marker.
func TestSyncer_RepeatDuringCommit(t *testing.T) {
	tests := []struct {
		name       string
		pauseAfter bool // stop the worker after its commit rather than before it
		remark     bool // a producer re-marks the repeat while the worker is stopped
	}{
		{name: "claim_still_held"},
		{name: "marker_rewritten", pauseAfter: true, remark: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			log := loggingtest.New(t, logging.Debug)

			source, raw := memorydb.New(), memorydb.New()
			commit := newBreakpoint()
			target := newBlockingDB(raw, commit, tt.pauseAfter)

			// A full batch, so it dispatches while the queue stays open.
			hashes := make([]common.Hash, maxHashesPerRequest)
			for i := range hashes {
				hashes[i] = writeCode(t, source, randomCode(t))
				require.NoError(t, customrawdb.WriteCodeToFetch(raw, hashes[i]))
			}
			repeat := hashes[0]

			// Already stored, so it is skipped rather than fetched. Receiving it
			// proves the batcher finished the iteration before it.
			barrier := writeCode(t, raw, randomCode(t))

			recorder := synctest.NewRecordingResponder(newResponder(log, source))
			// One batch is all this should ever cost, so a second request ends the
			// run instead of retrying against a peer that cannot satisfy it.
			guard := synctest.NewCancelAfter(recorder, 2, cancel)
			client := serve(t, ctx, log, guard)
			ch := make(chan common.Hash)
			syncErr := make(chan error, 1)
			go func() { syncErr <- NewSyncer(log, client, target, ch).Sync(ctx) }()

			for _, h := range hashes {
				send(t, ctx, ch, h)
			}
			// The guard cancels rather than the clock, so every rendezvous below
			// ends on the same signal instead of hanging.
			defer commit.resume()
			require.NoError(t, commit.wait(ctx), "the syncer never reached the commit")

			if tt.remark {
				require.NoError(t, customrawdb.WriteCodeToFetch(raw, repeat))
			}
			send(t, ctx, ch, repeat)
			send(t, ctx, ch, barrier)
			commit.resume()
			close(ch)
			require.False(t, guard.Fired(), "a repeat must not cost a second request")
			require.NoError(t, <-syncErr)

			requested := 0
			for _, n := range requestSizes(recorder) {
				requested += n
			}
			require.Equal(t, len(hashes), requested, "a repeat must not cost a second fetch")

			require.Equal(t, rawdb.ReadCode(source, repeat), rawdb.ReadCode(raw, repeat))
			it := customrawdb.NewCodeToFetchIterator(raw)
			defer it.Release()
			require.False(t, it.Next(), "every marker must be cleared")
		})
	}
}

// A failed write must fail the sync. Reporting success would drop the hash for
// the rest of the run, leaving the code unstored with nothing still saying it is
// owed.
func TestSyncer_WriteFailure(t *testing.T) {
	errBoom := errors.New("boom")

	tests := []struct {
		name    string
		stored  bool // already on disk, so the marker is cleared rather than fetched
		failing func(ethdb.KeyValueStore) *failingDB
	}{
		{
			name:   "clearing_a_stale_marker",
			stored: true,
			failing: func(inner ethdb.KeyValueStore) *failingDB {
				return &failingDB{KeyValueStore: inner, onDelete: errBoom}
			},
		},
		{
			name: "committing_fetched_code",
			failing: func(inner ethdb.KeyValueStore) *failingDB {
				return &failingDB{KeyValueStore: inner, onCommit: errBoom}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			log := loggingtest.New(t, logging.Debug)

			source, raw := memorydb.New(), memorydb.New()
			code := randomCode(t)
			hash := writeCode(t, source, code)
			if tt.stored {
				writeCode(t, raw, code)
			}
			require.NoError(t, customrawdb.WriteCodeToFetch(raw, hash))

			recorder := synctest.NewRecordingResponder(newResponder(log, source))
			// A swallowed failure would leave the hash owed and re-requested.
			guard := synctest.NewCancelAfter(recorder, 2, cancel)
			client := serve(t, ctx, log, guard)

			ch := make(chan common.Hash, 1)
			ch <- hash
			close(ch)

			err := NewSyncer(log, client, tt.failing(raw), ch).Sync(ctx)
			require.ErrorIs(t, err, errBoom)
			require.False(t, guard.Fired(), "the failure must end the run rather than retry")
		})
	}
}

// serve registers r on a single-node in-process network and returns a client
// bound to it.
func serve(t *testing.T, ctx context.Context, log logging.Logger, r handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]) *Client {
	t.Helper()
	net, tracker := synctest.ServeResponder(t, ctx, log, p2p.EVMCodeRequestHandlerID, r)
	return NewClient(net, tracker)
}

type codeRecorder = synctest.RecordingResponder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]

// requestSizes is the hash count of every request served, in order.
func requestSizes(c *codeRecorder) []int {
	reqs := c.Requests()
	sizes := make([]int, len(reqs))
	for i, req := range reqs {
		sizes[i] = len(req.GetHashes())
	}
	return sizes
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

// breakpoint stops the first goroutine to reach it, so a test can act while the
// code under test is held at a chosen point.
type breakpoint struct {
	stopOnce   sync.Once
	resumeOnce sync.Once
	hit        chan struct{}
	resumed    chan struct{}
}

func newBreakpoint() *breakpoint {
	return &breakpoint{hit: make(chan struct{}), resumed: make(chan struct{})}
}

// stop is called by the code under test.
func (b *breakpoint) stop() {
	b.stopOnce.Do(func() {
		close(b.hit)
		<-b.resumed
	})
}

// wait blocks until the code under test reaches the breakpoint, and reports why
// it never will once ctx ends.
func (b *breakpoint) wait(ctx context.Context) error {
	select {
	case <-b.hit:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// resume lets it continue. Idempotent, so a test can defer it and still resume
// at the point it meant to.
func (b *breakpoint) resume() { b.resumeOnce.Do(func() { close(b.resumed) }) }

// send hands h to the batcher, failing rather than blocking once the batcher has
// stopped consuming.
func send(t *testing.T, ctx context.Context, ch chan<- common.Hash, h common.Hash) {
	t.Helper()
	select {
	case ch <- h:
	case <-ctx.Done():
		t.Fatalf("the batcher stopped consuming: %v", ctx.Err())
	}
}

// blockingDB stops a worker on one side of its commit, so a test can drive a
// repeat through the batcher while the first copy is mid-flight.
type blockingDB struct {
	ethdb.KeyValueStore
	bp    *breakpoint
	after bool // stop once the commit has landed rather than before it
}

func newBlockingDB(inner ethdb.KeyValueStore, bp *breakpoint, after bool) *blockingDB {
	return &blockingDB{KeyValueStore: inner, bp: bp, after: after}
}

func (db *blockingDB) NewBatch() ethdb.Batch {
	return &blockingBatch{Batch: db.KeyValueStore.NewBatch(), db: db}
}

type blockingBatch struct {
	ethdb.Batch
	db *blockingDB
}

func (b *blockingBatch) Write() error {
	if !b.db.after {
		b.db.bp.stop()
	}
	err := b.Batch.Write()
	if b.db.after {
		b.db.bp.stop()
	}
	return err
}

// failingDB fails the marker delete, the commit, or neither, so a test can pick
// which of the syncer's two write paths breaks.
type failingDB struct {
	ethdb.KeyValueStore
	onDelete error
	onCommit error
}

func (db *failingDB) Delete(key []byte) error {
	if db.onDelete != nil {
		return db.onDelete
	}
	return db.KeyValueStore.Delete(key)
}

func (db *failingDB) NewBatch() ethdb.Batch {
	batch := db.KeyValueStore.NewBatch()
	if db.onCommit == nil {
		return batch
	}
	return &failingBatch{Batch: batch, err: db.onCommit}
}

type failingBatch struct {
	ethdb.Batch
	err error
}

func (b *failingBatch) Write() error {
	return b.err
}
