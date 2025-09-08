// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

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
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func Test_Server_GetRangeProof(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	smallTrieDB, err := generateTrieWithMinKeyLen(t, r, defaultRequestKeyLimit, 1)
	require.NoError(t, err)
	smallTrieRoot, err := smallTrieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	tests := []struct {
		name                     string
		request                  *pb.SyncGetRangeProofRequest
		expectedErr              *common.AppError
		expectedResponseLen      int
		expectedMaxResponseBytes int
		nodeID                   ids.NodeID
		proofNil                 bool
	}{
		{
			name: "proof too large",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 1000,
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "byteslimit is 0",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 0,
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "keylimit is 0",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   0,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "keys out of order",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
				StartKey:   &pb.MaybeBytes{Value: []byte{1}},
				EndKey:     &pb.MaybeBytes{Value: []byte{0}},
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "response bounded by key limit",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   2 * defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		{
			name: "response bounded by byte limit",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 2 * defaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
		},
		{
			name: "empty proof",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   ids.Empty[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			proofNil:    true,
			expectedErr: p2p.ErrUnexpected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			handler := NewGetRangeProofHandler(smallTrieDB)
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

			var proof merkledb.RangeProof
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

	serverDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(t, err)
	startRoot, err := serverDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	// create changes
	for x := 0; x < defaultRequestKeyLimit/2; x++ {
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
			merkledb.ViewChanges{BatchOps: ops},
		)
		require.NoError(t, err)
		require.NoError(t, view.CommitToDB(context.Background()))
	}

	endRoot, err := serverDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	fakeRootID := ids.GenerateTestID()

	tests := []struct {
		name                     string
		request                  *pb.SyncGetChangeProofRequest
		expectedErr              *common.AppError
		expectedResponseLen      int
		expectedMaxResponseBytes int
		nodeID                   ids.NodeID
		expectRangeProof         bool // Otherwise expect change proof
	}{
		{
			name: "proof restricted by BytesLimit",
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    10000,
			},
		},
		{
			name: "full response for small (single request) trie",
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		{
			name: "partial response to request for entire trie (full leaf limit)",
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		{
			name: "byteslimit is 0",
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    0,
			},
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "keylimit is 0",
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      0,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "keys out of order",
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
				StartKey:      &pb.MaybeBytes{Value: []byte{1}},
				EndKey:        &pb.MaybeBytes{Value: []byte{0}},
			},
			expectedErr: p2p.ErrUnexpected,
		},
		{
			name: "key limit too large",
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      2 * defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		{
			name: "bytes limit too large",
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    2 * defaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
		},
		{
			name: "insufficient history for change proof; return range proof",
			request: &pb.SyncGetChangeProofRequest{
				// This root doesn't exist so server has insufficient history
				// to serve a change proof
				StartRootHash: fakeRootID[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
			expectRangeProof:         true,
		},
		{
			name: "insufficient history for change proof or range proof",
			request: &pb.SyncGetChangeProofRequest{
				// These roots don't exist so server has insufficient history
				// to serve a change proof or range proof
				StartRootHash: ids.Empty[:],
				EndRootHash:   fakeRootID[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
			expectedErr:              p2p.ErrUnexpected,
		},
		{
			name: "empty proof",
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: fakeRootID[:],
				EndRootHash:   ids.Empty[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
			expectedErr:              p2p.ErrUnexpected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			handler := NewGetChangeProofHandler(serverDB)

			requestBytes, err := proto.Marshal(test.request)
			require.NoError(err)
			proofBytes, err := handler.AppRequest(context.Background(), test.nodeID, time.Time{}, requestBytes)
			require.ErrorIs(err, test.expectedErr)

			if test.expectedErr != nil {
				require.Nil(proofBytes)
				return
			}

			proofResult := &pb.SyncGetChangeProofResponse{}
			require.NoError(proto.Unmarshal(proofBytes, proofResult))

			if test.expectRangeProof {
				require.NotNil(proofResult.GetRangeProof())
			} else {
				require.NotNil(proofResult.GetChangeProof())
			}

			if test.expectedResponseLen > 0 {
				if test.expectRangeProof {
					var response merkledb.RangeProof
					require.NoError(response.UnmarshalBinary(proofResult.GetRangeProof()))
					require.LessOrEqual(len(response.KeyChanges), test.expectedResponseLen)
				} else {
					var response merkledb.ChangeProof
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
