// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"google.golang.org/protobuf/proto"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
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

	tests := map[string]struct {
		request                  *pb.SyncGetRangeProofRequest
		expectedErr              error
		expectedResponseLen      int
		expectedMaxResponseBytes int
		nodeID                   ids.NodeID
		proofNil                 bool
	}{
		"proof too large": {
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 1000,
			},
			proofNil:    true,
			expectedErr: ErrMinProofSizeIsTooLarge,
		},
		"byteslimit is 0": {
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 0,
			},
			proofNil: true,
		},
		"keylimit is 0": {
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 0,
			},
			proofNil: true,
		},
		"keys out of order": {
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
				StartKey:   []byte{1},
				EndKey:     []byte{0},
			},
			proofNil: true,
		},
		"key limit too large": {
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   2 * defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		"bytes limit too large": {
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 2 * defaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			sender := common.NewMockSender(ctrl)
			var proof *merkledb.RangeProof
			sender.EXPECT().SendAppResponse(
				gomock.Any(), // ctx
				gomock.Any(), // nodeID
				gomock.Any(), // requestID
				gomock.Any(), // responseBytes
			).DoAndReturn(
				func(_ context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
					// grab a copy of the proof so we can inspect it later
					if !test.proofNil {
						var proofProto pb.RangeProof
						require.NoError(proto.Unmarshal(responseBytes, &proofProto))

						var p merkledb.RangeProof
						require.NoError(p.UnmarshalProto(&proofProto))
						proof = &p
					}
					return nil
				},
			).AnyTimes()
			handler := NewNetworkServer(sender, smallTrieDB, logging.NoLog{})
			err := handler.HandleRangeProofRequest(context.Background(), test.nodeID, 0, test.request)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}
			if test.proofNil {
				require.Nil(proof)
				return
			}
			require.NotNil(proof)
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

func Test_Server_GetChangeProof(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	trieDB, _, err := generateTrieWithMinKeyLen(t, r, defaultRequestKeyLimit, 1)
	require.NoError(t, err)

	startRoot, err := trieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	// create changes
	for x := 0; x < 600; x++ {
		view, err := trieDB.NewView()
		require.NoError(t, err)

		key := make([]byte, r.Intn(100))
		_, err = r.Read(key)
		require.NoError(t, err)

		val := make([]byte, r.Intn(100))
		_, err = r.Read(val)
		require.NoError(t, err)

		require.NoError(t, view.Insert(context.Background(), key, val))

		deleteKeyStart := make([]byte, r.Intn(10))
		_, err = r.Read(deleteKeyStart)
		require.NoError(t, err)

		it := trieDB.NewIteratorWithStart(deleteKeyStart)
		if it.Next() {
			require.NoError(t, view.Remove(context.Background(), it.Key()))
		}
		require.NoError(t, it.Error())
		it.Release()

		require.NoError(t, view.CommitToDB(context.Background()))
	}

	endRoot, err := trieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	tests := map[string]struct {
		request                  *pb.SyncGetChangeProofRequest
		expectedErr              error
		expectedResponseLen      int
		expectedMaxResponseBytes int
		nodeID                   ids.NodeID
		proofNil                 bool
	}{
		"byteslimit is 0": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    0,
			},
			proofNil: true,
		},
		"keylimit is 0": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    0,
			},
			proofNil: true,
		},
		"keys out of order": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
				StartKey:      []byte{1},
				EndKey:        []byte{0},
			},
			proofNil: true,
		},
		"key limit too large": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      2 * defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		"bytes limit too large": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    2 * defaultRequestByteSizeLimit,
			},
			expectedMaxResponseBytes: defaultRequestByteSizeLimit,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			sender := common.NewMockSender(ctrl)
			var proofResult *merkledb.ChangeProof
			sender.EXPECT().SendAppResponse(
				gomock.Any(), // ctx
				gomock.Any(), // nodeID
				gomock.Any(), // requestID
				gomock.Any(), // responseBytes
			).DoAndReturn(
				func(_ context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
					// grab a copy of the proof so we can inspect it later
					if !test.proofNil {
						var responseProto pb.SyncGetChangeProofResponse
						require.NoError(proto.Unmarshal(responseBytes, &responseProto))

						// TODO when the client/server support including range proofs in the response,
						// this will need to be updated.
						var p merkledb.ChangeProof
						require.NoError(p.UnmarshalProto(responseProto.GetChangeProof()))
						proofResult = &p
					}
					return nil
				},
			).AnyTimes()
			handler := NewNetworkServer(sender, trieDB, logging.NoLog{})
			err := handler.HandleChangeProofRequest(context.Background(), test.nodeID, 0, test.request)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}
			if test.proofNil {
				require.Nil(proofResult)
				return
			}
			require.NotNil(proofResult)
			if test.expectedResponseLen > 0 {
				require.LessOrEqual(len(proofResult.KeyChanges), test.expectedResponseLen)
			}

			// TODO when the client/server support including range proofs in the response,
			// this will need to be updated.
			bytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
				Response: &pb.SyncGetChangeProofResponse_ChangeProof{
					ChangeProof: proofResult.ToProto(),
				},
			})
			require.NoError(err)
			require.LessOrEqual(len(bytes), int(test.request.BytesLimit))
			if test.expectedMaxResponseBytes > 0 {
				require.LessOrEqual(len(bytes), test.expectedMaxResponseBytes)
			}
		})
	}
}
