// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

// newTestHandlerMetrics returns [handlerMetrics] on a registry private to the
// test.
func newTestHandlerMetrics(tb testing.TB) *handlerMetrics {
	tb.Helper()
	m, err := newHandlerMetrics(prometheus.NewRegistry())
	require.NoError(tb, err, "newHandlerMetrics()")
	return m
}

func TestResponder(t *testing.T) {
	t.Parallel()

	codeHash, codeBytes := randomCode(t)
	otherHash, other := randomCode(t)

	db := memorydb.New()
	writeCode(db, codes{
		codeHash:  codeBytes,
		otherHash: other,
	})

	tests := []struct {
		name      string
		hashes    []common.Hash
		wantCodes [][]byte
		wantErr   *avacommon.AppError
	}{
		{
			name:      "single_hash",
			hashes:    []common.Hash{codeHash},
			wantCodes: [][]byte{codeBytes},
		},
		{
			name:      "multiple_hashes_preserve_order",
			hashes:    []common.Hash{codeHash, otherHash},
			wantCodes: [][]byte{codeBytes, other},
		},
		{
			name:    "missing_hash_rejected",
			hashes:  []common.Hash{{0xde, 0xad}},
			wantErr: errHashNotFound,
		},
		{
			// A client never needs the same code twice in one request, and
			// duplicates pad the response, so they are rejected as coreth does.
			name:    "duplicate_hashes_rejected",
			hashes:  []common.Hash{codeHash, codeHash},
			wantErr: errDuplicateHashes,
		},
		{
			name:    "too_many_hashes_rejected",
			hashes:  make([]common.Hash, maxHashesPerRequest+1),
			wantErr: errTooManyHashes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newResponder(loggingtest.New(t, logging.Debug), db, newTestHandlerMetrics(t))
			req := &syncpb.GetCodeRequest{Hashes: hashBytes(tt.hashes)}
			resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), req)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, resp, "a rejected request carries no response")
				return
			}
			require.Nil(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.wantCodes, resp.Data)
		})
	}
}

func TestErrorSentinels(t *testing.T) {
	t.Parallel()

	synctest.RequireDistinctAppErrors(t, map[string]*avacommon.AppError{
		"errTooManyHashes":   errTooManyHashes,
		"errHashNotFound":    errHashNotFound,
		"errDuplicateHashes": errDuplicateHashes,
	})
}
