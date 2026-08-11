// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

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
		perReq        int
		wantRequests  int
	}{
		{name: "empty"},
		{name: "single_blob", numFromSource: 1, wantRequests: 1},
		{name: "batches_across_requests", numFromSource: 40, perReq: 4, wantRequests: 10},
		{name: "skips_code_already_on_disk", numFromSource: 3, numOnDisk: 2, wantRequests: 1},
		// Shared bytecode puts the same hash on the queue many times.
		{name: "repeats_fetched_once", numFromSource: 1, copies: 200, perReq: 4, wantRequests: 1},
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
			counter := synctest.NewCountingResponder(newResponder(log, source))
			client := serve(t, ctx, log, counter)

			copies := max(tt.copies, 1)
			ch := make(chan common.Hash, len(want)*copies)
			for hash := range want {
				require.NoError(t, customrawdb.WriteCodeToFetch(target, hash))
				for range copies {
					ch <- hash
				}
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

			sizes := requestSizes(counter)
			require.Len(t, sizes, tt.wantRequests,
				"only hashes that are missing and not already claimed are requested, and a full batch is sent as its own request")
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

	c.release(batch)
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
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	log := loggingtest.New(t, logging.Debug)
	source := memorydb.New()
	hash := writeCode(t, source, randomCode(t))

	// Well-formed but the wrong code, so only the client's own verification can
	// reject it.
	responder := synctest.NewMutatingResponder(newResponder(log, source), -1, func(resp *syncpb.GetCodeResponse) {
		for i := range resp.GetData() {
			resp.Data[i] = []byte("tampered")
		}
	})
	client := serve(t, ctx, log, responder)

	got, err := getCode(ctx, log, client, []common.Hash{hash})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, got, "tampered code must never be accepted")
	require.Positive(t, responder.Served(), "the deadline must not expire before a tampered response is rejected")
}

// serve registers r on a single-node in-process network and returns a client
// bound to it.
func serve(t *testing.T, ctx context.Context, log logging.Logger, r handlers.Responder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]) *Client {
	t.Helper()
	net, tracker := synctest.ServeResponder(t, ctx, log, p2p.EVMCodeRequestHandlerID, r)
	return NewClient(net, tracker)
}

type codeCounter = synctest.CountingResponder[*syncpb.GetCodeRequest, *syncpb.GetCodeResponse]

// requestSizes is the hash count of every request served, in order.
func requestSizes(c *codeCounter) []int {
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
