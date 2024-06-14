// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

func newDefaultDBConfig() merkledb.Config {
	return merkledb.Config{
		IntermediateWriteBatchSize:  100,
		HistoryLength:               defaultRequestKeyLimit,
		ValueNodeCacheSize:          defaultRequestKeyLimit,
		IntermediateWriteBufferSize: defaultRequestKeyLimit,
		IntermediateNodeCacheSize:   defaultRequestKeyLimit,
		Reg:                         prometheus.NewRegistry(),
		Tracer:                      trace.Noop,
		BranchFactor:                merkledb.BranchFactor16,
	}
}

func TestGetRangeProof(t *testing.T) {
	t.Skip()
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	smallTrieKeyCount := defaultRequestKeyLimit
	smallTrieDB, _, err := generateTrieWithMinKeyLen(t, r, smallTrieKeyCount, 1)
	require.NoError(t, err)
	smallTrieRoot, err := smallTrieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	largeTrieKeyCount := 3 * defaultRequestKeyLimit
	largeTrieDB, largeTrieKeys, err := generateTrieWithMinKeyLen(t, r, largeTrieKeyCount, 1)
	require.NoError(t, err)
	largeTrieRoot, err := largeTrieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	tests := map[string]struct {
		db                  DB
		request             *pb.SyncGetRangeProofRequest
		modifyResponse      func(*merkledb.RangeProof)
		expectedErr         error
		expectedResponseLen int
	}{
		"proof restricted by BytesLimit": {
			db: smallTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 10000,
			},
		},
		"full response for small (single request) trie": {
			db: smallTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		"too many leaves in response": {
			db: smallTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   smallTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.KeyValues = append(response.KeyValues, merkledb.KeyValue{})
			},
			expectedErr: errTooManyKeys,
		},
		"partial response to request for entire trie (full leaf limit)": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   largeTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		"full response from near end of trie to end of trie (less than leaf limit)": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash: largeTrieRoot[:],
				StartKey: &pb.MaybeBytes{
					Value:     largeTrieKeys[len(largeTrieKeys)-30], // Set start 30 keys from the end of the large trie
					IsNothing: false,
				},
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			expectedResponseLen: 30,
		},
		"full response for intermediate range of trie (less than leaf limit)": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash: largeTrieRoot[:],
				StartKey: &pb.MaybeBytes{
					Value:     largeTrieKeys[1000], // Set the range for 1000 leafs in an intermediate range of the trie
					IsNothing: false,
				},
				EndKey:     &pb.MaybeBytes{Value: largeTrieKeys[1099]}, // (inclusive range)
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			expectedResponseLen: 100,
		},
		"removed first key in response": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   largeTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.KeyValues = response.KeyValues[1:]
			},
			expectedErr: merkledb.ErrInvalidProof,
		},
		"removed first key in response and replaced proof": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   largeTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				start := maybe.Some(response.KeyValues[1].Key)
				rootID, err := largeTrieDB.GetMerkleRoot(context.Background())
				require.NoError(t, err)
				proof, err := largeTrieDB.GetRangeProofAtRoot(context.Background(), rootID, start, maybe.Nothing[[]byte](), defaultRequestKeyLimit)
				require.NoError(t, err)
				response.KeyValues = proof.KeyValues
				response.StartProof = proof.StartProof
				response.EndProof = proof.EndProof
			},
			expectedErr: errInvalidRangeProof,
		},
		"removed key from middle of response": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   largeTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.KeyValues = append(response.KeyValues[:100], response.KeyValues[101:]...)
			},
			expectedErr: merkledb.ErrInvalidProof,
		},
		"start and end proof nodes removed": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   largeTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.StartProof = nil
				response.EndProof = nil
			},
			expectedErr: merkledb.ErrNoEndProof,
		},
		"end proof removed": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   largeTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.EndProof = nil
			},
			expectedErr: merkledb.ErrNoEndProof,
		},
		"empty proof": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   largeTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.StartProof = nil
				response.EndProof = nil
				response.KeyValues = nil
			},
			expectedErr: merkledb.ErrEmptyProof,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctx := context.Background()

			rangeProofHandler := NewSyncGetRangeProofHandler(logging.NoLog{}, test.db)
			serverHandler := p2p.TestHandler{
				AppRequestF: func(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
					responseBytes, err := rangeProofHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
					require.NoError(err)

					proofProto := &pb.RangeProof{}
					require.NoError(proto.Unmarshal(responseBytes, proofProto))

					proof := &merkledb.RangeProof{}
					require.NoError(proof.UnmarshalProto(proofProto))

					if test.modifyResponse != nil {
						test.modifyResponse(proof)
						responseBytes, err = proto.Marshal(proof.ToProto())
						require.NoError(err)
					}

					return responseBytes, nil
				},
			}

			done := make(chan struct{})
			client := p2ptest.NewClient(t, ctx, serverHandler)
			requestBytes, err := proto.Marshal(test.request)
			require.NoError(err)

			require.NoError(client.AppRequestAny(ctx, requestBytes, func(ctx context.Context, nodeID ids.NodeID, responseBytes []byte, err error) {
				defer close(done)

				require.NoError(err)

				proofProto := &pb.RangeProof{}
				require.NoError(proto.Unmarshal(responseBytes, proofProto))

				proof := &merkledb.RangeProof{}
				require.NoError(proof.UnmarshalProto(proofProto))

				require.Equal(test.expectedResponseLen, len(proof.KeyValues))

				bytes, err := proto.Marshal(proof.ToProto())
				require.NoError(err)

				require.Less(len(bytes), int(test.request.BytesLimit))
			}))

			<-done
		})
	}
}

//
//func TestGetChangeProof(t *testing.T) {
//	now := time.Now().UnixNano()
//	t.Logf("seed: %d", now)
//	r := rand.New(rand.NewSource(now)) // #nosec G404
//
//	serverDB, err := merkledb.New(
//		context.Background(),
//		memdb.New(),
//		newDefaultDBConfig(),
//	)
//	require.NoError(t, err)
//
//	clientDB, err := merkledb.New(
//		context.Background(),
//		memdb.New(),
//		newDefaultDBConfig(),
//	)
//	require.NoError(t, err)
//	startRoot, err := serverDB.GetMerkleRoot(context.Background())
//	require.NoError(t, err)
//
//	// create changes
//	for x := 0; x < defaultRequestKeyLimit/2; x++ {
//		ops := make([]database.BatchOp, 0, 11)
//		// add some key/values
//		for i := 0; i < 10; i++ {
//			key := make([]byte, r.Intn(100))
//			_, err = r.Read(key)
//			require.NoError(t, err)
//
//			val := make([]byte, r.Intn(100))
//			_, err = r.Read(val)
//			require.NoError(t, err)
//
//			ops = append(ops, database.BatchOp{Key: key, Value: val})
//		}
//
//		// delete a key
//		deleteKeyStart := make([]byte, r.Intn(10))
//		_, err = r.Read(deleteKeyStart)
//		require.NoError(t, err)
//
//		it := serverDB.NewIteratorWithStart(deleteKeyStart)
//		if it.Next() {
//			ops = append(ops, database.BatchOp{Key: it.Key(), Delete: true})
//		}
//		require.NoError(t, it.Error())
//		it.Release()
//
//		view, err := serverDB.NewView(
//			context.Background(),
//			merkledb.ViewChanges{BatchOps: ops},
//		)
//		require.NoError(t, err)
//		require.NoError(t, view.CommitToDB(context.Background()))
//	}
//
//	endRoot, err := serverDB.GetMerkleRoot(context.Background())
//	require.NoError(t, err)
//
//	fakeRootID := ids.GenerateTestID()
//
//	tests := map[string]struct {
//		db                        DB
//		request                   *pb.SyncGetChangeProofRequest
//		modifyChangeProofResponse func(*merkledb.ChangeProof)
//		modifyRangeProofResponse  func(*merkledb.RangeProof)
//		expectedErr               error
//		expectedResponseLen       int
//		expectRangeProof          bool // Otherwise expect change proof
//	}{
//		"proof restricted by BytesLimit": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    10000,
//			},
//		},
//		"full response for small (single request) trie": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			expectedResponseLen: defaultRequestKeyLimit,
//		},
//		"too many keys in response": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			modifyChangeProofResponse: func(response *merkledb.ChangeProof) {
//				response.KeyChanges = append(response.KeyChanges, make([]merkledb.KeyChange, defaultRequestKeyLimit)...)
//			},
//			expectedErr: errTooManyKeys,
//		},
//		"partial response to request for entire trie (full leaf limit)": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			expectedResponseLen: defaultRequestKeyLimit,
//		},
//		"removed first key in response": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			modifyChangeProofResponse: func(response *merkledb.ChangeProof) {
//				response.KeyChanges = response.KeyChanges[1:]
//			},
//			expectedErr: errInvalidChangeProof,
//		},
//		"removed key from middle of response": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			modifyChangeProofResponse: func(response *merkledb.ChangeProof) {
//				response.KeyChanges = append(response.KeyChanges[:100], response.KeyChanges[101:]...)
//			},
//			expectedErr: merkledb.ErrInvalidProof,
//		},
//		"all proof keys removed from response": {
//			request: &pb.SyncGetChangeProofRequest{
//				StartRootHash: startRoot[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			modifyChangeProofResponse: func(response *merkledb.ChangeProof) {
//				response.StartProof = nil
//				response.EndProof = nil
//			},
//			expectedErr: merkledb.ErrInvalidProof,
//		},
//		"range proof response; remove first key": {
//			request: &pb.SyncGetChangeProofRequest{
//				// Server doesn't have the (non-existent) start root
//				// so should respond with range proof.
//				StartRootHash: fakeRootID[:],
//				EndRootHash:   endRoot[:],
//				KeyLimit:      defaultRequestKeyLimit,
//				BytesLimit:    defaultRequestByteSizeLimit,
//			},
//			modifyChangeProofResponse: nil,
//			modifyRangeProofResponse: func(response *merkledb.RangeProof) {
//				response.KeyValues = response.KeyValues[1:]
//			},
//			expectedErr:      errInvalidRangeProof,
//			expectRangeProof: true,
//		},
//	}
//
//	for name, test := range tests {
//		t.Run(name, func(t *testing.T) {
//			require := require.New(t)
//
//			// Ensure test is well-formed.
//			if test.expectRangeProof {
//				require.Nil(test.modifyChangeProofResponse)
//			} else {
//				require.Nil(test.modifyRangeProofResponse)
//			}
//
//			changeOrRangeProof, err := sendChangeProofRequest(
//				t,
//				serverDB,
//				clientDB,
//				test.request,
//				1,
//				test.modifyChangeProofResponse,
//				test.modifyRangeProofResponse,
//			)
//			require.ErrorIs(err, test.expectedErr)
//			if test.expectedErr != nil {
//				return
//			}
//
//			if test.expectRangeProof {
//				require.NotNil(changeOrRangeProof.RangeProof)
//				require.Nil(changeOrRangeProof.ChangeProof)
//			} else {
//				require.NotNil(changeOrRangeProof.ChangeProof)
//				require.Nil(changeOrRangeProof.RangeProof)
//			}
//
//			if test.expectedResponseLen > 0 {
//				if test.expectRangeProof {
//					require.LessOrEqual(len(changeOrRangeProof.RangeProof.KeyValues), test.expectedResponseLen)
//				} else {
//					require.LessOrEqual(len(changeOrRangeProof.ChangeProof.KeyChanges), test.expectedResponseLen)
//				}
//			}
//
//			var bytes []byte
//			if test.expectRangeProof {
//				bytes, err = proto.Marshal(&pb.SyncGetChangeProofResponse{
//					Response: &pb.SyncGetChangeProofResponse_RangeProof{
//						RangeProof: changeOrRangeProof.RangeProof.ToProto(),
//					},
//				})
//			} else {
//				bytes, err = proto.Marshal(&pb.SyncGetChangeProofResponse{
//					Response: &pb.SyncGetChangeProofResponse_ChangeProof{
//						ChangeProof: changeOrRangeProof.ChangeProof.ToProto(),
//					},
//				})
//			}
//			require.NoError(err)
//			require.LessOrEqual(len(bytes), int(test.request.BytesLimit))
//		})
//	}
//}
//
//func TestRangeProofRetries(t *testing.T) {
//	now := time.Now().UnixNano()
//	t.Logf("seed: %d", now)
//	r := rand.New(rand.NewSource(now)) // #nosec G404
//	require := require.New(t)
//
//	keyCount := defaultRequestKeyLimit
//	db, _, err := generateTrieWithMinKeyLen(t, r, keyCount, 1)
//	require.NoError(err)
//	root, err := db.GetMerkleRoot(context.Background())
//	require.NoError(err)
//
//	maxRequests := 4
//	request := &pb.SyncGetRangeProofRequest{
//		RootHash:   root[:],
//		KeyLimit:   uint32(keyCount),
//		BytesLimit: defaultRequestByteSizeLimit,
//	}
//
//	responseCount := 0
//	modifyResponse := func(response *merkledb.RangeProof) {
//		responseCount++
//		if responseCount < maxRequests {
//			// corrupt the first [maxRequests] responses, to force the client to retryPriority.
//			response.KeyValues = nil
//		}
//	}
//	proof, err := sendRangeProofRequest(t, db, request, maxRequests, modifyResponse)
//	require.NoError(err)
//	require.Len(proof.KeyValues, keyCount)
//
//	require.Equal(responseCount, maxRequests) // check the client performed retries.
//}
