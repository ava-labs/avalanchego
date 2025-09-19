// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
	xsync "github.com/ava-labs/avalanchego/x/sync"
)

func Test_Server_GetRangeProof(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	smallTrieDB, err := generateTrieWithMinKeyLen(t, r, xsync.DefaultRequestKeyLimit, 1)
	require.NoError(t, err)
	smallTrieRoot, err := smallTrieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	tests := []struct {
		name                     string
		request                  *pb.GetRangeProofRequest
		expectedErr              *common.AppError
		expectedResponseLen      int
		expectedMaxResponseBytes int
		nodeID                   ids.NodeID
		proofNil                 bool
	}{
		{
			name: "proof too large",
			request: &pb.GetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   xsync.DefaultRequestKeyLimit,
				BytesLimit: 1000,
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "byteslimit is 0",
			request: &pb.GetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   xsync.DefaultRequestKeyLimit,
				BytesLimit: 0,
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "keylimit is 0",
			request: &pb.GetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   0,
				BytesLimit: xsync.DefaultRequestByteSizeLimit,
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "keys out of order",
			request: &pb.GetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   xsync.DefaultRequestKeyLimit,
				BytesLimit: xsync.DefaultRequestByteSizeLimit,
				StartKey:   &pb.MaybeBytes{Value: []byte{1}},
				EndKey:     &pb.MaybeBytes{Value: []byte{0}},
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "response bounded by key limit",
			request: &pb.GetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   2 * xsync.DefaultRequestKeyLimit,
				BytesLimit: xsync.DefaultRequestByteSizeLimit,
			},
			expectedResponseLen: xsync.DefaultRequestKeyLimit,
		},
		{
			name: "response bounded by byte limit",
			request: &pb.GetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   xsync.DefaultRequestKeyLimit,
				BytesLimit: 2 * xsync.DefaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: xsync.DefaultRequestByteSizeLimit,
		},
		{
			name: "empty proof",
			request: &pb.GetRangeProofRequest{
				RootHash:   ids.Empty[:],
				KeyLimit:   xsync.DefaultRequestKeyLimit,
				BytesLimit: xsync.DefaultRequestByteSizeLimit,
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			handler := xsync.NewGetRangeProofHandler(smallTrieDB)
			requestBytes, err := proto.Marshal(test.request)
			require.NoError(err)
			responseBytes, err := handler.AppRequest(context.Background(), test.nodeID, time.Time{}, requestBytes)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}
			if test.proofNil {
				require.Nil(responseBytes)
				return
			}

			var proof RangeProof
			require.NoError(proof.UnmarshalBinary(responseBytes))

			if test.expectedResponseLen > 0 {
				require.LessOrEqual(len(proof.KeyChanges), test.expectedResponseLen)
			}

			bytes, err := proof.MarshalBinary()
			require.NoError(err)
			require.LessOrEqual(len(bytes), int(test.request.BytesLimit))
			if test.expectedMaxResponseBytes > 0 {
				require.LessOrEqual(len(bytes), test.expectedMaxResponseBytes)
			}
		})
	}
}

func Test_Server_GetChangeProof(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	serverDB, err := New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(t, err)
	startRoot, err := serverDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	// create changes
	for x := 0; x < xsync.DefaultRequestKeyLimit/2; x++ {
		ops := make([]database.BatchOp, 0, 11)
		// add some key/values
		for i := 0; i < 10; i++ {
			key := make([]byte, r.Intn(100))
			_, err = r.Read(key)
			require.NoError(t, err)

			val := make([]byte, r.Intn(100))
			_, err = r.Read(val)
			require.NoError(t, err)

			ops = append(ops, database.BatchOp{Key: key, Value: val})
		}

		// delete a key
		deleteKeyStart := make([]byte, r.Intn(10))
		_, err = r.Read(deleteKeyStart)
		require.NoError(t, err)

		it := serverDB.NewIteratorWithStart(deleteKeyStart)
		if it.Next() {
			ops = append(ops, database.BatchOp{Key: it.Key(), Delete: true})
		}
		require.NoError(t, it.Error())
		it.Release()

		view, err := serverDB.NewView(
			context.Background(),
			ViewChanges{BatchOps: ops},
		)
		require.NoError(t, err)
		require.NoError(t, view.CommitToDB(context.Background()))
	}

	endRoot, err := serverDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	fakeRootID := ids.GenerateTestID()

	tests := []struct {
		name                     string
		request                  *pb.GetChangeProofRequest
		expectedErr              *common.AppError
		expectedResponseLen      int
		expectedMaxResponseBytes int
		nodeID                   ids.NodeID
		expectRangeProof         bool // Otherwise expect change proof
	}{
		{
			name: "proof restricted by BytesLimit",
			request: &pb.GetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      xsync.DefaultRequestKeyLimit,
				BytesLimit:    10000,
			},
		},
		{
			name: "full response for small (single request) trie",
			request: &pb.GetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      xsync.DefaultRequestKeyLimit,
				BytesLimit:    xsync.DefaultRequestByteSizeLimit,
			},
			expectedResponseLen: xsync.DefaultRequestKeyLimit,
		},
		{
			name: "partial response to request for entire trie (full leaf limit)",
			request: &pb.GetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      xsync.DefaultRequestKeyLimit,
				BytesLimit:    xsync.DefaultRequestByteSizeLimit,
			},
			expectedResponseLen: xsync.DefaultRequestKeyLimit,
		},
		{
			name: "byteslimit is 0",
			request: &pb.GetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      xsync.DefaultRequestKeyLimit,
				BytesLimit:    0,
			},
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "keylimit is 0",
			request: &pb.GetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      0,
				BytesLimit:    xsync.DefaultRequestByteSizeLimit,
			},
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "keys out of order",
			request: &pb.GetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      xsync.DefaultRequestKeyLimit,
				BytesLimit:    xsync.DefaultRequestByteSizeLimit,
				StartKey:      &pb.MaybeBytes{Value: []byte{1}},
				EndKey:        &pb.MaybeBytes{Value: []byte{0}},
			},
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "key limit too large",
			request: &pb.GetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      2 * xsync.DefaultRequestKeyLimit,
				BytesLimit:    xsync.DefaultRequestByteSizeLimit,
			},
			expectedResponseLen: xsync.DefaultRequestKeyLimit,
		},
		{
			name: "bytes limit too large",
			request: &pb.GetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      xsync.DefaultRequestKeyLimit,
				BytesLimit:    2 * xsync.DefaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: xsync.DefaultRequestByteSizeLimit,
		},
		{
			name: "insufficient history for change proof; return range proof",
			request: &pb.GetChangeProofRequest{
				// This root doesn't exist so server has insufficient history
				// to serve a change proof
				StartRootHash: fakeRootID[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      xsync.DefaultRequestKeyLimit,
				BytesLimit:    xsync.DefaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: xsync.DefaultRequestByteSizeLimit,
			expectRangeProof:         true,
		},
		{
			name: "insufficient history for change proof or range proof",
			request: &pb.GetChangeProofRequest{
				// These roots don't exist so server has insufficient history
				// to serve a change proof or range proof
				StartRootHash: ids.Empty[:],
				EndRootHash:   fakeRootID[:],
				KeyLimit:      xsync.DefaultRequestKeyLimit,
				BytesLimit:    xsync.DefaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: xsync.DefaultRequestByteSizeLimit,
			expectedErr:              p2p.ErrUnexpected,
		},
		{
			name: "empty proof",
			request: &pb.GetChangeProofRequest{
				StartRootHash: fakeRootID[:],
				EndRootHash:   ids.Empty[:],
				KeyLimit:      xsync.DefaultRequestKeyLimit,
				BytesLimit:    xsync.DefaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: xsync.DefaultRequestByteSizeLimit,
			expectedErr:              p2p.ErrUnexpected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			handler := xsync.NewGetChangeProofHandler(serverDB)

			requestBytes, err := proto.Marshal(test.request)
			require.NoError(err)
			proofBytes, err := handler.AppRequest(context.Background(), test.nodeID, time.Time{}, requestBytes)
			require.ErrorIs(err, test.expectedErr)

			if test.expectedErr != nil {
				require.Nil(proofBytes)
				return
			}

			proofResult := &pb.GetChangeProofResponse{}
			require.NoError(proto.Unmarshal(proofBytes, proofResult))

			if test.expectRangeProof {
				require.NotNil(proofResult.GetRangeProof())
			} else {
				require.NotNil(proofResult.GetChangeProof())
			}

			if test.expectedResponseLen > 0 {
				if test.expectRangeProof {
					var response RangeProof
					require.NoError(response.UnmarshalBinary(proofResult.GetRangeProof()))
					require.LessOrEqual(len(response.KeyChanges), test.expectedResponseLen)
				} else {
					var response ChangeProof
					require.NoError(response.UnmarshalBinary(proofResult.GetChangeProof()))
					require.LessOrEqual(len(response.KeyChanges), test.expectedResponseLen)
				}
			}

			require.LessOrEqual(len(proofBytes), int(test.request.BytesLimit))
			if test.expectedMaxResponseBytes > 0 {
				require.LessOrEqual(len(proofBytes), test.expectedMaxResponseBytes)
			}
		})
	}
}
