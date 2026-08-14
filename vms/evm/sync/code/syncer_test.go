// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

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

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

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
			// A size check would reject this forever, since re-requesting cannot
			// change the answer. This case fails if one comes back.
			name:   "oversized_but_honest",
			hashes: []common.Hash{oversizedHash},
			data:   [][]byte{oversized},
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
			// A broken skip re-requests forever, so end the run.
			guard := synctest.NewCancelAfter(recorder, wantRequests+1, cancel)
			client := serve(t, ctx, log, guard)

			syncer, err := NewSyncer(log, client, target)
			require.NoError(t, err)

			copies := max(tt.copies, 1)
			for hash := range want {
				for range copies {
					require.NoError(t, syncer.AddCode(ctx, []common.Hash{hash}))
				}
			}
			// Only what is missing is owed.
			require.Len(t, markedHashes(t, target), tt.numFromSource,
				"code already stored must never be marked")

			syncer.CloseInput()

			err = syncer.Sync(ctx)
			require.False(t, guard.Fired(), "the syncer requested more than batching explains")
			require.NoError(t, err)

			for hash, code := range want {
				require.Equal(t, code, rawdb.ReadCode(target, hash))
			}

			require.Empty(t, markedHashes(t, target), "all to-fetch markers must be cleared")

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

	// A distinct hash per claim, so the count stays under what one byte holds.
	const claims = 100

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
	// Enough bad answers to show a rejection is not a one-off.
	const tampered = 2

	ctx := t.Context()
	log := loggingtest.New(t, logging.Debug)
	source := memorydb.New()
	code := randomCode(t)
	hash := writeCode(t, source, code)

	// Well-formed but wrong, so only the client's verification can reject it.
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

// However input closed, a later AddCode must be refused and leave no marker.
// A marker left behind is code owed with nothing running to fetch it.
func TestSyncer_InputClosed(t *testing.T) {
	tests := []struct {
		name  string
		close func(t *testing.T, s *Syncer)
	}{
		{
			name:  "by_the_producer",
			close: func(_ *testing.T, s *Syncer) { s.CloseInput() },
		},
		{
			// Cancelled rather than drained, so only Sync's exit can close input.
			name: "by_sync_exiting",
			close: func(t *testing.T, s *Syncer) {
				ctx, cancel := context.WithCancel(t.Context())
				syncErr := make(chan error, 1)
				go func() { syncErr <- s.Sync(ctx) }()
				cancel()
				require.ErrorIs(t, <-syncErr, context.Canceled)
				require.ErrorIs(t, s.Sync(t.Context()), errSyncAlreadyRun)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := loggingtest.New(t, logging.Debug)
			db := memorydb.New()
			syncer, err := NewSyncer(log, nil, db)
			require.NoError(t, err)

			tt.close(t, syncer)

			// A live context, so the refusal is input's, not the caller's.
			require.ErrorIs(t, syncer.AddCode(t.Context(), []common.Hash{{1}}), ErrInputClosed)
			require.Empty(t, markedHashes(t, db), "a refused AddCode must not leave a marker behind")
		})
	}
}

// AddCode racing CloseInput has two outcomes: accepted and marked, or refused 
// and unmarked. A marker left by a refused call is code owed to nobody.
func TestSyncer_AddCodeRacesCloseInput(t *testing.T) {
	const producers = 50

	log := loggingtest.New(t, logging.Debug)
	db := memorydb.New()
	syncer, err := NewSyncer(log, nil, db)
	require.NoError(t, err)

	hashes := make([]common.Hash, producers)
	for i := range hashes {
		hashes[i] = common.Hash{byte(i + 1)}
	}

	// One slot per producer, so each writes its own and Wait orders the reads.
	errs := make([]error, producers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, codeHash := range hashes {
		wg.Go(func() {
			<-start
			errs[i] = syncer.AddCode(t.Context(), []common.Hash{codeHash})
		})
	}
	wg.Go(func() {
		<-start
		syncer.CloseInput()
	})

	close(start)
	wg.Wait()

	var accepted []common.Hash
	for i, err := range errs {
		if err == nil {
			accepted = append(accepted, hashes[i])
			continue
		}
		require.ErrorIs(t, err, ErrInputClosed, "AddCode is either accepted or refused as closed")
	}

	require.ElementsMatch(t, accepted, markedHashes(t, db),
		"a marker exists exactly when AddCode accepted the hash")
}

// markedHashes is every hash currently recorded as owed.
func markedHashes(t *testing.T, db ethdb.Iteratee) []common.Hash {
	t.Helper()
	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	var marked []common.Hash
	for it.Next() {
		marked = append(marked, common.BytesToHash(it.Key()[len(customrawdb.CodeToFetchPrefix):]))
	}
	require.NoError(t, it.Error())
	return marked
}

// An interrupted run leaves markers, and the next syncer must fetch what is
// still owed, not only clear what arrived.
func TestSyncer_ResumesFromMarkers(t *testing.T) {
	ctx := t.Context()
	log := loggingtest.New(t, logging.Debug)
	source, target := memorydb.New(), memorydb.New()

	// Owed, so it must be fetched.
	owed := writeRandomCode(t, source)
	require.NoError(t, customrawdb.WriteCodeToFetch(target, owed))

	// Marked but already stored, so its marker is cleared without a request.
	arrived := writeRandomCode(t, target)
	require.NoError(t, customrawdb.WriteCodeToFetch(target, arrived))

	recorder := synctest.NewRecordingResponder(newResponder(log, source))
	client := serve(t, ctx, log, recorder)

	syncer, err := NewSyncer(log, client, target)
	require.NoError(t, err)
	syncer.CloseInput()
	require.NoError(t, syncer.Sync(ctx))

	require.Equal(t, rawdb.ReadCode(source, owed), rawdb.ReadCode(target, owed))
	require.Equal(t, []int{1}, requestSizes(recorder), "only the hash still owed is requested")

	require.Empty(t, markedHashes(t, target), "recovery must clear every marker it resolves")
}

// A repeat arriving while its first copy is being fetched must cost neither a
// second request nor a leaked marker.
func TestSyncer_RepeatDuringFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	log := loggingtest.New(t, logging.Debug)
	source, target := memorydb.New(), memorydb.New()

	// A full batch, so it dispatches while input stays open.
	hashes := make([]common.Hash, maxHashesPerRequest)
	for i := range hashes {
		hashes[i] = writeRandomCode(t, source)
	}
	repeat := hashes[0]

	held := newHoldingResponder(newResponder(log, source))
	recorder := synctest.NewRecordingResponder(held)
	// One request is all this should cost, so a second ends the run.
	guard := synctest.NewCancelAfter(recorder, 2, cancel)
	client := serve(t, ctx, log, guard)

	syncer, err := NewSyncer(log, client, target)
	require.NoError(t, err)
	require.NoError(t, syncer.AddCode(ctx, hashes))

	syncErr := make(chan error, 1)
	go func() { syncErr <- syncer.Sync(ctx) }()

	defer held.release()
	require.NoError(t, held.wait(ctx), "the syncer never sent a request")

	// Outstanding, so the claim on every hash in the request is held.
	require.NoError(t, syncer.AddCode(ctx, []common.Hash{repeat}))
	syncer.CloseInput()
	held.release()
	require.NoError(t, <-syncErr)

	require.False(t, guard.Fired(), "a repeat must not cost a second request")
	require.Equal(t, []int{len(hashes)}, requestSizes(recorder),
		"a repeat must not cost a second fetch")
	require.Equal(t, rawdb.ReadCode(source, repeat), rawdb.ReadCode(target, repeat))
	require.Empty(t, markedHashes(t, target), "every marker must be cleared")
}

// holdingResponder holds its first response until released.
type holdingResponder struct {
	inner       handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]
	holding     chan struct{}
	released    chan struct{}
	first       atomic.Bool
	releaseOnce sync.Once
}

func newHoldingResponder(inner handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]) *holdingResponder {
	return &holdingResponder{
		inner:    inner,
		holding:  make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (h *holdingResponder) Respond(ctx context.Context, nodeID ids.NodeID, req *syncpb.GetCodeRequest) (*syncpb.GetCodeResponse, *avacommon.AppError) {
	// Later requests pass through, so a test asserting there is no second one
	// still finishes.
	if !h.first.Swap(true) {
		close(h.holding)
		<-h.released
	}
	return h.inner.Respond(ctx, nodeID, req)
}

// wait blocks until a request is outstanding.
func (h *holdingResponder) wait(ctx context.Context) error {
	select {
	case <-h.holding:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release lets the held response through. Idempotent, so a test can defer it.
func (h *holdingResponder) release() {
	h.releaseOnce.Do(func() {
		close(h.released)
	})
}

// A failed write must fail the run. Reporting success would leave the code
// unstored with nothing saying it is owed.
func TestSyncer_WriteFailure(t *testing.T) {
	errBoom := errors.New("boom")

	t.Run("clearing_recovered_markers", func(t *testing.T) {
		log := loggingtest.New(t, logging.Debug)
		raw := memorydb.New()
		// Marked and stored, so recovery clears the marker and commits.
		hash := writeRandomCode(t, raw)
		require.NoError(t, customrawdb.WriteCodeToFetch(raw, hash))

		_, err := NewSyncer(log, nil, &failingDB{KeyValueStore: raw, err: errBoom})
		require.ErrorIs(t, err, errBoom)
	})

	t.Run("committing_fetched_code", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		log := loggingtest.New(t, logging.Debug)

		source, raw := memorydb.New(), memorydb.New()
		hash := writeRandomCode(t, source)

		recorder := synctest.NewRecordingResponder(newResponder(log, source))
		// A swallowed failure would leave the hash owed and re-requested.
		guard := synctest.NewCancelAfter(recorder, 2, cancel)
		client := serve(t, ctx, log, guard)

		db := &failingDB{KeyValueStore: raw, err: errBoom, onlyCode: true}
		syncer, err := NewSyncer(log, client, db)
		require.NoError(t, err)
		require.NoError(t, syncer.AddCode(ctx, []common.Hash{hash}))
		syncer.CloseInput()

		require.ErrorIs(t, syncer.Sync(ctx), errBoom)
		require.False(t, guard.Fired(), "the failure must end the run rather than retry")
	})
}

// serve registers r on a single-node in-process network.
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

// writeRandomCode stores a fresh blob and returns its hash.
func writeRandomCode(t *testing.T, db ethdb.KeyValueWriter) common.Hash {
	t.Helper()
	return writeCode(t, db, randomCode(t))
}

func randomCode(t *testing.T) []byte {
	t.Helper()
	code := make([]byte, 128)
	_, err := rand.Read(code)
	require.NoError(t, err)
	return code
}

// failingDB fails a commit, chosen by what the batch carries. Only fetched code
// writes bytecode, so onlyCode picks the worker's commit.
type failingDB struct {
	ethdb.KeyValueStore
	err      error
	onlyCode bool
}

func (db *failingDB) NewBatch() ethdb.Batch {
	return &failingBatch{Batch: db.KeyValueStore.NewBatch(), db: db}
}

type failingBatch struct {
	ethdb.Batch
	db       *failingDB
	hasBytes bool
}

func (b *failingBatch) Put(key, value []byte) error {
	if bytes.HasPrefix(key, rawdb.CodePrefix) {
		b.hasBytes = true
	}
	return b.Batch.Put(key, value)
}

func (b *failingBatch) Write() error {
	if b.db.onlyCode && !b.hasBytes {
		return b.Batch.Write()
	}
	return b.db.err
}
