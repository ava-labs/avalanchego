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
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/evm/sync/customrawdb"
	"github.com/ava-labs/avalanchego/vms/evm/sync/synctest"

	syncpb "github.com/ava-labs/avalanchego/proto/pb/sync"
	avacommon "github.com/ava-labs/avalanchego/snow/engine/common"
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
			name:    "count mismatch",
			hashes:  []common.Hash{hash},
			data:    [][]byte{},
			wantErr: errCodeCountMismatch,
		},
		{
			name:    "hash mismatch",
			hashes:  []common.Hash{hash},
			data:    [][]byte{[]byte("tampered")},
			wantErr: errCodeHashMismatch,
		},
		{
			name:    "size exceeded",
			hashes:  []common.Hash{oversizedHash},
			data:    [][]byte{oversized},
			wantErr: errCodeSizeExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyCode(tt.hashes, tt.data)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSyncer(t *testing.T) {
	tests := []struct {
		name          string
		numFromSource int
		numOnDisk     int
	}{
		{name: "single blob", numFromSource: 1},
		{name: "batches across requests", numFromSource: 12},
		{name: "skips code already on disk", numFromSource: 3, numOnDisk: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A broken skip re-requests forever, so bound the wait.
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			nodeID := ids.GenerateTestNodeID()

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

			net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
			require.NoError(t, RegisterHandler(net, logging.NoLog{}, source))

			ch := make(chan common.Hash, len(want))
			for hash := range want {
				require.NoError(t, customrawdb.WriteCodeToFetch(target, hash))
				ch <- hash
			}
			close(ch)

			require.NoError(t, NewSyncer(NewClient(net, tracker), target, ch).Sync(ctx))

			for hash, code := range want {
				require.Equal(t, code, rawdb.ReadCode(target, hash))
			}

			it := customrawdb.NewCodeToFetchIterator(target)
			defer it.Release()
			require.False(t, it.Next(), "all to-fetch markers must be cleared")
		})
	}
}

func TestSyncer_RejectsTamperedResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	nodeID := ids.GenerateTestNodeID()

	hash := crypto.Keccak256Hash([]byte("real code"))

	net, tracker := synctest.NewSelfNetwork(t, ctx, nodeID)
	require.NoError(t, net.AddHandler(p2p.EVMCodeRequestHandlerID, tamperingHandler()))

	got, err := getCode(ctx, NewClient(net, tracker), []common.Hash{hash})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Nil(t, got, "tampered code must never be accepted")
}

// tamperingHandler returns well-formed but wrong code, so verification always fails.
func tamperingHandler() p2p.Handler {
	return p2p.TestHandler{
		AppRequestF: func(_ context.Context, _ ids.NodeID, _ time.Time, requestBytes []byte) ([]byte, *avacommon.AppError) {
			req := &syncpb.GetCodeRequest{}
			if err := proto.Unmarshal(requestBytes, req); err != nil {
				return nil, avacommon.ErrUndefined
			}
			data := make([][]byte, len(req.GetHashes()))
			for i := range data {
				data[i] = []byte("tampered")
			}
			respBytes, err := proto.Marshal(&syncpb.GetCodeResponse{Data: data})
			if err != nil {
				return nil, avacommon.ErrUndefined
			}
			return respBytes, nil
		},
	}
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
