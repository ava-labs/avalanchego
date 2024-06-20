// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func Test_Server_GetRangeProof(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	smallTrieDB, _, err := generateTrieWithMinKeyLen(t, r, defaultRequestKeyLimit, 1)
	require.NoError(t, err)
	smallTrieRoot, err := smallTrieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	tests := []struct {
		name                     string
		request                  *pb.SyncGetRangeProofRequest
		expectedErr              error
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
			expectedErr: ErrMinProofSizeIsTooLarge,
		},
		{
			name: "byteslimit is 0",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 0,
			},
			proofNil:    true,
			expectedErr: errInvalidBytesLimit,
		},
		{
			name: "keylimit is 0",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   0,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			proofNil:    true,
			expectedErr: errInvalidKeyLimit,
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
			expectedErr: errInvalidBounds,
		},
		{
			name: "key limit too large",
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   2 * defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		{
			name: "bytes limit too large",
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
			expectedErr: merkledb.ErrEmptyProof,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			handler := NewSyncGetRangeProofHandler(logging.NoLog{}, smallTrieDB)
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
			require.NotNil(responseBytes)

			var proof *merkledb.RangeProof
			if !test.proofNil {
				var proofProto pb.RangeProof
				require.NoError(proto.Unmarshal(responseBytes, &proofProto))

				var p merkledb.RangeProof
				require.NoError(p.UnmarshalProto(&proofProto))
				proof = &p
			}

			if test.expectedResponseLen > 0 {
				require.LessOrEqual(len(proof.KeyValues), test.expectedResponseLen)
			}

			bytes, err := proto.Marshal(proof.ToProto())
			require.NoError(err)
			require.LessOrEqual(len(bytes), int(test.request.BytesLimit))
			if test.expectedMaxResponseBytes > 0 {
				require.LessOrEqual(len(bytes), test.expectedMaxResponseBytes)
			}
		})
	}
}

//
//func Test_Server_GetChangeProof(t *testing.T) {
//	now := time.Now().UnixNano()
//	t.Logf("seed: %d", now)
//	r := rand.New(rand.NewSource(now)) // #nosec G404
//	trieDB, _, err := generateTrieWithMinKeyLen(t, r, defaultRequestKeyLimit, 1)
//	require.NoError(t, err)
//
//	startRoot, err := trieDB.GetMerkleRoot(context.Background())
//	require.NoError(t, err)
//
//	// create changes
//	ops := make([]database.BatchOp, 0, 300)
//	for x := 0; x < 300; x++ {
//		key := make([]byte, r.Intn(100))
//		_, err = r.Read(key)
//		require.NoError(t, err)
//
//		val := make([]byte, r.Intn(100))
//		_, err = r.Read(val)
//		require.NoError(t, err)
//
//		ops = append(ops, database.BatchOp{Key: key, Value: val})
//
//		deleteKeyStart := make([]byte, r.Intn(10))
//		_, err = r.Read(deleteKeyStart)
//		require.NoError(t, err)
//
//		it := trieDB.NewIteratorWithStart(deleteKeyStart)
//		if it.Next() {
//			ops = append(ops, database.BatchOp{Key: it.Key(), Delete: true})
//		}
//		require.NoError(t, it.Error())
//		it.Release()
//
//		view, err := trieDB.NewView(
//			context.Background(),
//			merkledb.ViewChanges{BatchOps: ops},
//		)
//		require.NoError(t, err)
//		require.NoError(t, view.CommitToDB(context.Background()))
//	}
//
//	endRoot, err := trieDB.GetMerkleRoot(context.Background())
//	require.NoError(t, err)
//
//	fakeRootID := ids.GenerateTestID()
//
//	tests := map[string]struct {
//		request                  *pb.SyncGetChangeProofRequest
//		expectedErr              error
//		expectedResponseLen      int
//		expectedMaxResponseBytes int
//		nodeID                   ids.NodeID
//		proofNil                 bool
//		expectRangeProof         bool // Otherwise expect change proof
//	}{
//		"byteslimit is 0": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    0,
//			},
//			proofNil: true,
//		},
//		"keylimit is 0": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    0,
//			},
//			proofNil: true,
//		},
//		"keys out of order": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//				StartKey:      &pb.MaybeBytes{Value: []byte{1}},
//				EndKey:        &pb.MaybeBytes{Value: []byte{0}},
//			},
//			proofNil: true,
//		},
//		"key limit too large": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      2 * defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			expectedResponseLen: defaultRequestKeyLimit,
//		},
//		"bytes limit too large": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    2 * defaultRequestByteSizeLimit,
//			},
//			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
//		},
//		"insufficient history for change proof; return range proof": {
//			request: &pb.SyncGetChangeProofRequest{
//				// This root doesn't exist so server has insufficient history
//				// to serve a change proof
//				StartRootHash: fakeRootID[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
//			expectRangeProof:         true,
//		},
//		"insufficient history for change proof or range proof": {
//			request: &pb.SyncGetChangeProofRequest{
//				// These roots don't exist so server has insufficient history
//				// to serve a change proof or range proof
//				StartRootHash: ids.Empty[:],
//				EndRootHash:   fakeRootID[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
//			proofNil:                 true,
//		},
//		"empt proof": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: fakeRootID[:],
//				EndRootHash:   ids.Empty[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
//			proofNil:                 true,
//		},
//	}
//
//	for name, test := range tests {
//		t.Run(name, func(t *testing.T) {
//			require := require.New(t)
//			ctrl := gomock.NewController(t)
//			defer ctrl.Finish()
//
//			// Store proof returned by server in [proofResult]
//			var proofResult *pb.SyncGetChangeProofResponse
//			var proofBytes []byte
//			sender := common.NewMockSender(ctrl)
//			sender.EXPECT().SendAppResponse(
//				gomock.Any(), // ctx
//				gomock.Any(), // nodeID
//				gomock.Any(), // requestID
//				gomock.Any(), // responseBytes
//			).DoAndReturn(
//				func(_ context.Context, _ ids.NodeID, _ uint32, responseBytes []byte) error {
//					if test.proofNil {
//						return nil
//					}
//					proofBytes = responseBytes
//
//					// grab a copy of the proof so we can inspect it later
//					var responseProto pb.SyncGetChangeProofResponse
//					require.NoError(proto.Unmarshal(responseBytes, &responseProto))
//					proofResult = &responseProto
//
//					return nil
//				},
//			).AnyTimes()
//
//			handler := NewNetworkServer(sender, trieDB, logging.NoLog{})
//			err := handler.HandleChangeProofRequest(context.Background(), test.nodeID, 0, test.request)
//			require.ErrorIs(err, test.expectedErr)
//			if test.expectedErr != nil {
//				return
//			}
//
//			if test.proofNil {
//				require.Nil(proofResult)
//				return
//			}
//			require.NotNil(proofResult)
//
//			if test.expectRangeProof {
//				require.NotNil(proofResult.GetRangeProof())
//			} else {
//				require.NotNil(proofResult.GetChangeProof())
//			}
//
//			if test.expectedResponseLen > 0 {
//				if test.expectRangeProof {
//					require.LessOrEqual(len(proofResult.GetRangeProof().KeyValues), test.expectedResponseLen)
//				} else {
//					require.LessOrEqual(len(proofResult.GetChangeProof().KeyChanges), test.expectedResponseLen)
//				}
//			}
//
//			require.NoError(err)
//			require.LessOrEqual(len(proofBytes), int(test.request.BytesLimit))
//			if test.expectedMaxResponseBytes > 0 {
//				require.LessOrEqual(len(proofBytes), test.expectedMaxResponseBytes)
//			}
//		})
//	}
//}
