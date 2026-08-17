// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
)

func TestMain(m *testing.M) {
	// Importing saetest pulls in libevm packages that start goroutines at init,
	// so ignore what exists before any test runs, not what a test leaks.
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}

func TestVerifyCodePrefix(t *testing.T) {
	code := []byte("contract bytecode")
	hash := crypto.Keccak256Hash(code)
	other := []byte("another blob")
	otherHash := crypto.Keccak256Hash(other)

	oversized := make([]byte, params.MaxCodeSize+1)
	oversizedHash := crypto.Keccak256Hash(oversized)

	tests := []struct {
		name    string
		hashes  []common.Hash
		data    [][]byte
		wantN   int
		wantErr error
	}{
		{
			name:   "valid",
			hashes: []common.Hash{hash},
			data:   [][]byte{code},
			wantN:  1,
		},
		{
			// A peer answers fewer hashes instead of failing the whole request
			// when the rest would not fit in one message.
			name:   "valid_partial",
			hashes: []common.Hash{hash, otherHash},
			data:   [][]byte{code},
			wantN:  1,
		},
		{
			name:    "empty_response",
			hashes:  []common.Hash{hash},
			data:    [][]byte{},
			wantErr: errCodeCountMismatch,
		},
		{
			name:    "more_than_requested",
			hashes:  []common.Hash{hash},
			data:    [][]byte{code, other},
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
			wantN:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := verifyCodePrefix(tt.hashes, tt.data)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr == nil {
				require.Equal(t, tt.wantN, n)
			}
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
		{name: "repeats_at_batch_boundary_fetched_once", numFromSource: maxHashesPerRequest, copies: 2},
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

			// Added while the syncer runs, so the batcher is woken mid-flight.
			copies := max(tt.copies, 1)
			var eg errgroup.Group
			eg.Go(func() error {
				defer syncer.CloseInput()
				for hash := range want {
					for range copies {
						if err := syncer.AddCode(ctx, []common.Hash{hash}); err != nil {
							return err
						}
					}
				}
				return nil
			})
			eg.Go(func() error { return syncer.Sync(ctx) })

			err = eg.Wait()
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
				// An overgrown batch costs the whole request, since the handler drops it.
				require.LessOrEqual(t, size, maxHashesPerRequest, "a request outgrew the batch size")
				requested += size
			}
			require.Equal(t, tt.numFromSource, requested, "every missing hash is requested once")
		})
	}
}

// Code already on disk is neither marked nor requested, so a resumed sync takes
// no ownership of what it has.
func TestSyncer_SkipsStoredCode(t *testing.T) {
	ctx := t.Context()
	log := loggingtest.New(t, logging.Debug)
	source, target := memorydb.New(), memorydb.New()

	missing := writeRandomCode(t, source)
	stored := writeRandomCode(t, target)

	recorder := synctest.NewRecordingResponder(newResponder(log, source))
	syncer, err := NewSyncer(log, serve(t, ctx, log, recorder), target)
	require.NoError(t, err)
	require.NoError(t, syncer.AddCode(ctx, []common.Hash{missing, stored}))

	require.Equal(t, []common.Hash{missing}, markedHashes(t, target),
		"only the missing hash is owed")

	syncer.CloseInput()
	require.NoError(t, syncer.Sync(ctx))
	require.Equal(t, []int{1}, requestSizes(recorder), "stored code is never requested")
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

func TestSyncer_RepeatsOfStoredCodeNeverClaim(t *testing.T) {
	const (
		numHashes = 50
		producers = 8
	)

	log := loggingtest.New(t, logging.Debug)
	db := memorydb.New()

	stored := make([]common.Hash, numHashes)
	for i := range stored {
		stored[i] = writeRandomCode(t, db)
	}

	syncer, err := NewSyncer(log, nil, db)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range producers {
		wg.Go(func() {
			assert.NoError(t, syncer.AddCode(t.Context(), stored))
		})
	}
	wg.Wait()

	require.Zero(t, held(syncer.claimed), "already-stored code must never be claimed")
	require.Empty(t, markedHashes(t, db), "already-stored code must never be marked")
}

// A write failure must release its claim, or the hash is stuck forever.
func TestSyncer_ClaimReleasedOnWriteFailure(t *testing.T) {
	log := loggingtest.New(t, logging.Debug)
	// Fails on the first op.
	db := evmdb.New(saetest.NewFlakyDB(memdb.New(), 0))

	syncer, err := NewSyncer(log, nil, db)
	require.NoError(t, err)

	hash := common.Hash{1}
	require.ErrorIs(t, syncer.AddCode(t.Context(), []common.Hash{hash}), saetest.ErrInjected)
	require.Zero(t, held(syncer.claimed), "a failed write must not leave a claim behind")
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

// A truncated response must resume for the rest, not retry the whole request.
func TestSyncer_ResumesAfterPartialResponse(t *testing.T) {
	ctx := t.Context()
	log := loggingtest.New(t, logging.Debug)
	source := memorydb.New()

	hashes := make([]common.Hash, 3)
	codes := make([][]byte, 3)
	for i := range hashes {
		codes[i] = randomCode(t)
		hashes[i] = writeCode(t, source, codes[i])
	}

	// Truncated to one entry, regardless of request size.
	responder := synctest.NewMutatingResponder(newResponder(log, source), 1, func(resp *syncpb.GetCodeResponse) {
		resp.Data = resp.Data[:1]
	})
	recorder := synctest.NewRecordingResponder(responder)
	client := serve(t, ctx, log, recorder)

	data, err := getCode(ctx, log, client, hashes)
	require.NoError(t, err)
	require.Equal(t, codes, data)

	require.Len(t, requestSizes(recorder), 2, "a truncated response costs exactly one resumed request")
}

// A hash too large for any peer to answer must fail fast, not retry forever.
func TestSyncer_RejectsUnfetchableHash(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	log := loggingtest.New(t, logging.Debug)
	source := memorydb.New()

	code := randomCode(t)
	hash := writeCode(t, source, code)

	r := newResponder(log, source)
	r.sizeBudget = len(code) - 1 // even this one hash cannot fit

	// A correct reject costs one request. A second means errors.Is missed it
	// and getCode is retrying, so cut the run instead of hanging on it.
	guard := synctest.NewCancelAfter(r, 2, cancel)
	client := serve(t, ctx, log, guard)

	_, err := getCode(ctx, log, client, []common.Hash{hash})
	require.False(t, guard.Fired(), "errCodeTooLarge must stop getCode after one request")
	require.ErrorIs(t, err, errCodeTooLarge)
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

func TestSyncer_DrainedQueueMeansEmpty(t *testing.T) {
	t.Parallel()

	log := loggingtest.New(t, logging.Debug)

	tests := []struct {
		name          string
		rounds        int
		producers     int
		hashesPerCall int
		dbFails       bool
		wantAccepted  bool // whether any AddCode is expected to succeed at all
	}{
		{name: "one_producer", rounds: 20000, producers: 1, hashesPerCall: 1, wantAccepted: true},
		{name: "many_producers", rounds: 2000, producers: 8, hashesPerCall: 1, wantAccepted: true},
		{name: "batched_add_code", rounds: 2000, producers: 4, hashesPerCall: 5, wantAccepted: true},
		{name: "db_fails", rounds: 100, producers: 4, hashesPerCall: 3, dbFails: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			offered := make([]common.Hash, tt.hashesPerCall)
			for i := range offered {
				offered[i] = common.Hash{byte(i + 1)}
			}

			everAccepted := false
			for round := range tt.rounds {
				db := ethdb.KeyValueStore(memorydb.New())
				if tt.dbFails {
					// Fails from the first op, so every write in this round is rejected.
					db = evmdb.New(saetest.NewFlakyDB(memdb.New(), 0))
				}
				syncer, err := NewSyncer(log, nil, db)
				require.NoError(t, err)

				// Buffered to the producer count, so no AddCode waits to report.
				results := make(chan error, tt.producers)

				var wg sync.WaitGroup
				for range tt.producers {
					wg.Go(func() {
						results <- syncer.AddCode(t.Context(), offered)
					})
				}
				wg.Go(syncer.CloseInput)

				// Sleeping on an empty drain keeps this from starving the producers.
				drained := 0
				for {
					taken, closed := syncer.q.take()
					drained += len(taken)
					if len(taken) > 0 {
						continue
					}
					if closed {
						break
					}
					require.NoError(t, syncer.q.wait(t.Context()))
				}
				wg.Wait()
				close(results)

				// Same hashes for every producer, so nil just means admitted.
				accepted := false
				for err := range results {
					if err == nil {
						accepted = true
						continue
					}
					require.Truef(t, errors.Is(err, ErrInputClosed) || errors.Is(err, saetest.ErrInjected),
						"round %d: AddCode refused with an unexpected error: %v", round, err)
				}

				wantDrained := 0
				if accepted {
					wantDrained = len(offered)
				}
				require.Equalf(t, wantDrained, drained,
					"round %d: a hash stayed queued after the drain reported empty and closed", round)
				everAccepted = everAccepted || accepted
			}

			require.Equal(t, tt.wantAccepted, everAccepted,
				"whether any AddCode is accepted is what the case is built to exercise")
		})
	}
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

// Concurrent repeats of an in-flight hash must cost no extra fetch.
func TestSyncer_RepeatDuringFetch(t *testing.T) {
	const concurrentRepeats = 20

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

	// Held in flight, so concurrent repeats must defer to it.
	var wg sync.WaitGroup
	for range concurrentRepeats {
		wg.Go(func() {
			assert.NoError(t, syncer.AddCode(ctx, []common.Hash{repeat}))
		})
	}
	wg.Wait()

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
	// Later requests pass through, so a no-second-request assertion still finishes.
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

// A crash at any single write must leave the store recoverable by a fresh syncer
// plus a re-add of whatever AddCode refused.
func TestSyncer_WriteFailure(t *testing.T) {
	log := loggingtest.New(t, logging.Debug)
	source := memorydb.New()

	// A full batch and a short one, so concurrent worker commits are in the stream.
	hashes := make([]common.Hash, maxHashesPerRequest+1)
	want := map[common.Hash][]byte{}
	for i := range hashes {
		code := randomCode(t)
		hashes[i] = writeCode(t, source, code)
		want[hashes[i]] = code
	}
	// Already stored on the target, so a stale-marker clear is in the stream too.
	storedCode := randomCode(t)
	storedHash := crypto.Keccak256Hash(storedCode)
	want[storedHash] = storedCode

	// Markers a previous run left, so recovery has a clear and a re-queue to
	// crash during rather than an empty batch.
	resumed := append([]common.Hash{storedHash}, hashes[:3]...)
	hashes = hashes[3:]

	// A clean run counts the ops, so the sweep can crash at every one of them.
	ops := func() int {
		counter := saetest.NewFlakyDB(newSeededDB(t, storedCode, resumed), math.MaxInt)
		_, err := runSync(t, log, source, evmdb.New(counter), hashes)
		require.NoError(t, err)
		return counter.Calls()
	}()
	require.Positive(t, ops)

	for failAfter := range ops {
		t.Run(fmt.Sprintf("fail_after_%d", failAfter), func(t *testing.T) {
			raw := newSeededDB(t, storedCode, resumed)
			flaky := evmdb.New(saetest.NewFlakyDB(raw, failAfter))

			// Only an accepted AddCode promises a durable marker.
			reAdd, err := runSync(t, log, source, flaky, hashes)
			if err != nil {
				require.ErrorIs(t, err, saetest.ErrInjected)
			}

			// A second syncer over the same store, now healthy, must converge.
			target := evmdb.New(raw)
			_, err = runSync(t, log, source, target, reAdd)
			require.NoError(t, err)

			for hash, code := range want {
				require.Equalf(t, code, rawdb.ReadCode(target, hash), "code for %s", hash)
			}
			require.Empty(t, markedHashes(t, target), "every marker must be cleared")
		})
	}
}

// newSeededDB returns a store holding stored code and a previous run's markers,
// seeded before any fault injector wraps it.
func newSeededDB(t *testing.T, stored []byte, marked []common.Hash) database.Database {
	t.Helper()
	raw := memdb.New()
	db := evmdb.New(raw)
	writeCode(t, db, stored)
	for _, codeHash := range marked {
		require.NoError(t, customrawdb.WriteCodeToFetch(db, codeHash))
	}
	return raw
}

// runSync adds hashes and runs one syncer over db, returning what it refused.
func runSync(t *testing.T, log logging.Logger, source, db ethdb.KeyValueStore, hashes []common.Hash) ([]common.Hash, error) {
	t.Helper()
	ctx := t.Context()

	syncer, err := NewSyncer(log, serve(t, ctx, log, newResponder(log, source)), db)
	if err != nil {
		return hashes, err
	}
	if err := syncer.AddCode(ctx, hashes); err != nil {
		return hashes, err
	}

	// Sync returns once input closes and the queue drains, so close first.
	syncer.CloseInput()
	return nil, syncer.Sync(ctx)
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
