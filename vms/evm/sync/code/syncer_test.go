// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"crypto/rand"
	"fmt"
	"math"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	evmdb "github.com/ava-labs/avalanchego/vms/evm/database"
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
			recorder := synctest.NewRecordingResponder(newResponder(log, source))
			client := serve(t, ctx, log, recorder)

			s, err := NewSyncer(log, client, target)
			require.NoError(t, err)

			var eg errgroup.Group
			eg.Go(func() error {
				for hash := range want {
					for range max(tt.copies, 1) {
						if err := s.AddCode([]common.Hash{hash}, true); err != nil {
							return err
						}
					}
				}
				return s.AddCode(nil, false)
			})
			eg.Go(func() error {
				return s.Sync(ctx)
			})
			require.NoError(t, eg.Wait())

			assertDBSynced(t, target, want)

			requests := recorder.Requests()
			// Only the trailing batch can be short, so the count follows
			// directly.
			wantRequests := (tt.numFromSource + maxHashesPerRequest - 1) / maxHashesPerRequest
			require.Len(t, requests, wantRequests,
				"only hashes that are missing and not already tracked are requested, and a full batch is sent as its own request")
			requested := 0
			for _, req := range requests {
				reqSize := len(req.GetHashes())
				// The handler drops a request over the cap, so an overgrown
				// batch costs the whole request, not just the excess.
				require.LessOrEqual(t, reqSize, maxHashesPerRequest, "a request outgrew the batch size")
				requested += reqSize
			}
			require.Equal(t, tt.numFromSource, requested, "every missing hash is requested once")
		})
	}
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

// A failed write must fail the sync rather than report success, and the
// surviving markers, plus a re-add of anything AddCode never accepted, must let
// a fresh syncer finish the job.
func TestSyncer_WriteFailure(t *testing.T) {
	log := loggingtest.New(t, logging.Debug)
	source := memorydb.New()

	// A full batch and a short one, so commits from concurrent workers are in
	// the op stream.
	hashes := make([]common.Hash, maxHashesPerRequest+1)
	want := map[common.Hash][]byte{}
	for i := range hashes {
		code := randomCode(t)
		hashes[i] = writeCode(t, source, code)
		want[hashes[i]] = code
	}
	// Already stored, so the stale-marker clear is in the op stream too.
	storedCode := randomCode(t)
	storedHash := crypto.Keccak256Hash(storedCode)
	hashes = append(hashes, storedHash)
	want[storedHash] = storedCode

	// A clean run counts the mutating ops, so the sweep below can crash the
	// syncer at every one of them.
	ops := func() int {
		ctx := t.Context()
		client := serve(t, ctx, log, newResponder(log, source))

		raw := memdb.New()
		target := evmdb.New(raw)
		rawdb.WriteCode(target, storedHash, storedCode)
		flaky := saetest.NewFlakyDB(raw, math.MaxInt)

		s, err := NewSyncer(log, client, evmdb.New(flaky))
		require.NoError(t, err)
		require.NoError(t, s.AddCode(hashes, false))
		require.NoError(t, s.Sync(ctx))
		return flaky.Calls()
	}()

	for failAfter := range ops {
		t.Run(fmt.Sprintf("failAfter_%d", failAfter), func(t *testing.T) {
			ctx := t.Context()
			client := serve(t, ctx, log, newResponder(log, source))

			raw := memdb.New()
			target := evmdb.New(raw)
			rawdb.WriteCode(target, storedHash, storedCode)

			s, err := NewSyncer(log, client, evmdb.New(saetest.NewFlakyDB(raw, failAfter)))
			require.NoError(t, err)

			// Only a successful AddCode promises a durable marker, so hashes it
			// never accepted are the caller's responsibility to re-add after
			// the crash.
			var reAdd []common.Hash
			if err := s.AddCode(hashes, false); err != nil {
				require.ErrorIs(t, err, saetest.ErrInjected)
				reAdd = hashes
			} else {
				require.ErrorIs(t, s.Sync(ctx), saetest.ErrInjected)
			}

			// Everything else must reach the recovered syncer through the
			// markers it reads back from disk.
			recovered, err := NewSyncer(log, client, target)
			require.NoError(t, err)
			require.NoError(t, recovered.AddCode(reAdd, false))
			require.NoError(t, recovered.Sync(ctx))
			assertDBSynced(t, target, want)
		})
	}
}

// A call after the queue is closed must error rather than panic.
func TestSyncer_AddCodeAfterClose(t *testing.T) {
	log := loggingtest.New(t, logging.Debug)
	db := memorydb.New()
	s, err := NewSyncer(log, nil, db)
	require.NoError(t, err)

	require.NoError(t, s.AddCode(nil, false))

	hash := crypto.Keccak256Hash(randomCode(t))
	require.ErrorIs(t, s.AddCode([]common.Hash{hash}, true), errUnexpectedCode)
	require.ErrorIs(t, s.AddCode(nil, false), errUnexpectedCode)
}

// serve registers r on a single-node in-process network and returns a client
// bound to it.
func serve(
	t *testing.T,
	ctx context.Context,
	log logging.Logger,
	r handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse],
) *Client {
	t.Helper()
	return NewClient(
		synctest.ServeResponder(t, ctx, log, p2p.EVMCodeRequestHandlerID, r),
	)
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

func assertDBSynced(tb testing.TB, db ethdb.KeyValueStore, want map[common.Hash][]byte) {
	tb.Helper()

	for hash, code := range want {
		require.Equalf(tb, code, rawdb.ReadCode(db, hash), "code for hash %s must be persisted", hash)
	}

	it := customrawdb.NewCodeToFetchIterator(db)
	defer it.Release()

	require.False(tb, it.Next(), "all to-fetch markers must be cleared")
}
