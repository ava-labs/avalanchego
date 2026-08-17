// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
)

func TestResponder(t *testing.T) {
	t.Parallel()

	db := memorydb.New()
	codeBytes := []byte("contract bytecode")
	codeHash := writeCode(t, db, codeBytes)

	other := randomCode(t)
	otherHash := writeCode(t, db, other)

	tests := []struct {
		name     string
		hashes   []common.Hash
		budget   int // zero means the default budget
		wantData [][]byte
		wantErr  *avacommon.AppError
	}{
		{
			name:     "single_hash",
			hashes:   []common.Hash{codeHash},
			wantData: [][]byte{codeBytes},
		},
		{
			name:     "multiple_hashes_preserve_order",
			hashes:   []common.Hash{codeHash, otherHash},
			wantData: [][]byte{codeBytes, other},
		},
		{
			name:    "missing_hash_rejected",
			hashes:  []common.Hash{{0xde, 0xad}},
			wantErr: errHashNotFound,
		},
		{
			name:     "duplicate_hashes_served",
			hashes:   []common.Hash{codeHash, codeHash},
			wantData: [][]byte{codeBytes, codeBytes},
		},
		{
			name:    "too_many_hashes_rejected",
			hashes:  make([]common.Hash, maxHashesPerRequest+1),
			wantErr: errTooManyHashes,
		},
		{
			// A prefix is a valid partial answer, so the client can resume
			// for whatever it left out instead of failing the whole request.
			name:     "size_bounded_prefix",
			hashes:   []common.Hash{codeHash, otherHash},
			budget:   len(codeBytes),
			wantData: [][]byte{codeBytes},
		},
		{
			// Even alone this cannot fit, so no prefix trick helps: the peer
			// rejects it outright instead of trying to send it.
			name:    "single_hash_too_large_rejected",
			hashes:  []common.Hash{codeHash},
			budget:  len(codeBytes) - 1,
			wantErr: errCodeTooLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newResponder(loggingtest.New(t, logging.Debug), db)
			if tt.budget > 0 {
				r.sizeBudget = tt.budget
			}

			rawHashes := make([][]byte, len(tt.hashes))
			for i, h := range tt.hashes {
				rawHashes[i] = h.Bytes()
			}
			resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetCodeRequest{Hashes: rawHashes})

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, resp, "a rejected request carries no response")
				return
			}
			require.Nil(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.wantData, resp.Data)
		})
	}
}

func TestErrorSentinels(t *testing.T) {
	t.Parallel()

	synctest.RequireDistinctAppErrors(t, map[string]*avacommon.AppError{
		"errTooManyHashes": errTooManyHashes,
		"errHashNotFound":  errHashNotFound,
		"errCodeTooLarge":  errCodeTooLarge,
	})
}
