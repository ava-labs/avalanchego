// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"

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
		wantData [][]byte
		wantErr  *avacommon.AppError
	}{
		{
			name:     "single hash",
			hashes:   []common.Hash{codeHash},
			wantData: [][]byte{codeBytes},
		},
		{
			name:     "multiple hashes preserve order",
			hashes:   []common.Hash{codeHash, otherHash},
			wantData: [][]byte{codeBytes, other},
		},
		{
			name:    "missing hash rejected",
			hashes:  []common.Hash{{0xde, 0xad}},
			wantErr: errHashNotFound,
		},
		{
			name:     "duplicate hashes served",
			hashes:   []common.Hash{codeHash, codeHash},
			wantData: [][]byte{codeBytes, codeBytes},
		},
		{
			name:    "too many hashes rejected",
			hashes:  make([]common.Hash, maxHashesPerRequest+1),
			wantErr: errTooManyHashes,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newResponder(logging.NoLog{}, db)

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

// A code is the identity of an [avacommon.AppError], so each sentinel must be
// positive and must not collide with the framework or with [handlers].
func TestErrorSentinels(t *testing.T) {
	t.Parallel()

	sentinels := map[string]*avacommon.AppError{
		"errTooManyHashes": errTooManyHashes,
		"errHashNotFound":  errHashNotFound,
	}
	foreign := []*avacommon.AppError{
		handlers.ErrMalformedRequest,
		handlers.ErrMarshalResponse,
		p2p.ErrUnexpected,
		p2p.ErrUnregisteredHandler,
		p2p.ErrNotValidator,
		p2p.ErrThrottled,
		avacommon.ErrUndefined,
		avacommon.ErrTimeout,
	}

	seen := make(map[int32]string, len(sentinels))
	for name, sentinel := range sentinels {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, sentinel, &avacommon.AppError{Code: sentinel.Code})
			require.Positive(t, sentinel.Code, "p2p and the engine own the non-positive codes")

			for _, f := range foreign {
				require.NotErrorIs(t, sentinel, f)
			}
		})

		other, dup := seen[sentinel.Code]
		require.Falsef(t, dup, "%s and %s share code %d", name, other, sentinel.Code)
		seen[sentinel.Code] = name
	}
}
