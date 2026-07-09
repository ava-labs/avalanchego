// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package code

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/ethdb/memorydb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
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
		wantDrop bool
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
			name:     "missing hash drops",
			hashes:   []common.Hash{{0xde, 0xad}},
			wantDrop: true,
		},
		{
			name:     "duplicate hashes drop",
			hashes:   []common.Hash{codeHash, codeHash},
			wantDrop: true,
		},
		{
			name:     "too many hashes drops",
			hashes:   []common.Hash{{1}, {2}, {3}, {4}, {5}, {6}},
			wantDrop: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newResponder(db)

			rawHashes := make([][]byte, len(tt.hashes))
			for i, h := range tt.hashes {
				rawHashes[i] = h.Bytes()
			}
			resp, err := r.Respond(t.Context(), ids.GenerateTestNodeID(), &syncpb.GetCodeRequest{Hashes: rawHashes})
			require.NoError(t, err)

			if tt.wantDrop {
				require.Nil(t, resp)
			} else {
				require.NotNil(t, resp)
				require.Equal(t, tt.wantData, resp.Data)
			}
		})
	}
}
