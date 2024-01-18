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

	"go.uber.org/mock/gomock"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/trace"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/maybe"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
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

// Create a client and send a range proof request to a server
// whose underlying database is [serverDB].
// The server's response is modified with [modifyResponse] before
// being returned to the server.
// The client makes at most [maxAttempts] attempts to fulfill
// the request before returning an error.
func sendRangeProofRequest(
	t *testing.T,
	serverDB DB,
	request *pb.SyncGetRangeProofRequest,
	maxAttempts int,
	modifyResponse func(*merkledb.RangeProof),
) (*merkledb.RangeProof, error) {
	t.Helper()

	require := require.New(t)
	ctrl := gomock.NewController(t)

	var (
		// Number of calls from the client to the server so far.
		numAttempts int

		// Sends messages from server to client.
		sender = common.NewMockSender(ctrl)

		// Serves the range proof.
		server = NewNetworkServer(sender, serverDB, logging.NoLog{})

		clientNodeID, serverNodeID = ids.GenerateTestNodeID(), ids.GenerateTestNodeID()

		// "Sends" the request from the client to the server and
		// "receives" the response from the server. In reality,
		// it just invokes the server's method and receives
		// the response on [serverResponseChan].
		networkClient = NewMockNetworkClient(ctrl)

		serverResponseChan = make(chan []byte, 1)

		// The context used in client.GetRangeProof.
		// Canceled after the first response is received because
		// the client will keep sending requests until its context
		// expires or it succeeds.
		ctx, cancel = context.WithCancel(context.Background())
	)

	defer cancel()

	// The client fetching a range proof.
	client, err := NewClient(&ClientConfig{
		NetworkClient: networkClient,
		Metrics:       &mockMetrics{},
		Log:           logging.NoLog{},
		BranchFactor:  merkledb.BranchFactor16,
	})
	require.NoError(err)

	networkClient.EXPECT().RequestAny(
		gomock.Any(), // ctx
		gomock.Any(), // min version
		gomock.Any(), // request
	).DoAndReturn(
		func(_ context.Context, _ *version.Application, request []byte) (ids.NodeID, []byte, error) {
			go func() {
				// Get response from server
				require.NoError(server.AppRequest(context.Background(), clientNodeID, 0, time.Now().Add(time.Hour), request))
			}()

			// Wait for response from server
			serverResponse := <-serverResponseChan

			numAttempts++

			if numAttempts >= maxAttempts {
				defer cancel()
			}

			return serverNodeID, serverResponse, nil
		},
	).AnyTimes()

	// The server should expect to "send" a response to the client.
	sender.EXPECT().SendAppResponse(
		gomock.Any(), // ctx
		clientNodeID,
		gomock.Any(), // requestID
		gomock.Any(), // responseBytes
	).DoAndReturn(
		func(_ context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
			// deserialize the response so we can modify it if needed.
			var responseProto pb.RangeProof
			require.NoError(proto.Unmarshal(responseBytes, &responseProto))

			var response merkledb.RangeProof
			require.NoError(response.UnmarshalProto(&responseProto))

			// modify if needed
			if modifyResponse != nil {
				modifyResponse(&response)
			}

			// reserialize the response and pass it to the client to complete the handling.
			responseBytes, err := proto.Marshal(response.ToProto())
			require.NoError(err)

			serverResponseChan <- responseBytes

			return nil
		},
	).AnyTimes()

	return client.GetRangeProof(ctx, request)
}

func TestGetRangeProof(t *testing.T) {
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
			proof, err := sendRangeProofRequest(t, test.db, test.request, 1, test.modifyResponse)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}
			if test.expectedResponseLen > 0 {
				require.Len(proof.KeyValues, test.expectedResponseLen)
			}
			bytes, err := proto.Marshal(proof.ToProto())
			require.NoError(err)
			require.Less(len(bytes), int(test.request.BytesLimit))
		})
	}
}

func sendChangeProofRequest(
	t *testing.T,
	serverDB DB,
	clientDB DB,
	request *pb.SyncGetChangeProofRequest,
	maxAttempts int,
	modifyChangeProof func(*merkledb.ChangeProof),
	modifyRangeProof func(*merkledb.RangeProof),
) (*merkledb.ChangeOrRangeProof, error) {
	t.Helper()

	require := require.New(t)
	ctrl := gomock.NewController(t)

	var (
		// Number of calls from the client to the server so far.
		numAttempts int

		// Sends messages from server to client.
		sender = common.NewMockSender(ctrl)

		// Serves the change proof.
		server = NewNetworkServer(sender, serverDB, logging.NoLog{})

		clientNodeID, serverNodeID = ids.GenerateTestNodeID(), ids.GenerateTestNodeID()

		// "Sends" the request from the client to the server and
		// "receives" the response from the server. In reality,
		// it just invokes the server's method and receives
		// the response on [serverResponseChan].
		networkClient = NewMockNetworkClient(ctrl)

		serverResponseChan = make(chan []byte, 1)

		// The context used in client.GetChangeProof.
		// Canceled after the first response is received because
		// the client will keep sending requests until its context
		// expires or it succeeds.
		ctx, cancel = context.WithCancel(context.Background())
	)

	// The client fetching a change proof.
	client, err := NewClient(&ClientConfig{
		NetworkClient: networkClient,
		Metrics:       &mockMetrics{},
		Log:           logging.NoLog{},
		BranchFactor:  merkledb.BranchFactor16,
	})
	require.NoError(err)

	defer cancel() // avoid leaking a goroutine

	networkClient.EXPECT().RequestAny(
		gomock.Any(), // ctx
		gomock.Any(), // min version
		gomock.Any(), // request
	).DoAndReturn(
		func(_ context.Context, _ *version.Application, request []byte) (ids.NodeID, []byte, error) {
			go func() {
				// Get response from server
				require.NoError(server.AppRequest(context.Background(), clientNodeID, 0, time.Now().Add(time.Hour), request))
			}()

			// Wait for response from server
			serverResponse := <-serverResponseChan

			numAttempts++

			if numAttempts >= maxAttempts {
				defer cancel()
			}

			return serverNodeID, serverResponse, nil
		},
	).AnyTimes()

	// Expect server (serverDB) to send app response to client (clientDB)
	sender.EXPECT().SendAppResponse(
		gomock.Any(), // ctx
		clientNodeID,
		gomock.Any(), // requestID
		gomock.Any(), // responseBytes
	).DoAndReturn(
		func(_ context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
			// deserialize the response so we can modify it if needed.
			var responseProto pb.SyncGetChangeProofResponse
			require.NoError(proto.Unmarshal(responseBytes, &responseProto))

			if responseProto.GetChangeProof() != nil {
				// Server responded with a change proof
				var changeProof merkledb.ChangeProof
				require.NoError(changeProof.UnmarshalProto(responseProto.GetChangeProof()))

				// modify if needed
				if modifyChangeProof != nil {
					modifyChangeProof(&changeProof)
				}

				// reserialize the response and pass it to the client to complete the handling.
				responseBytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
					Response: &pb.SyncGetChangeProofResponse_ChangeProof{
						ChangeProof: changeProof.ToProto(),
					},
				})
				require.NoError(err)

				serverResponseChan <- responseBytes

				return nil
			}

			// Server responded with a range proof
			var rangeProof merkledb.RangeProof
			require.NoError(rangeProof.UnmarshalProto(responseProto.GetRangeProof()))

			// modify if needed
			if modifyRangeProof != nil {
				modifyRangeProof(&rangeProof)
			}

			// reserialize the response and pass it to the client to complete the handling.
			responseBytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
				Response: &pb.SyncGetChangeProofResponse_RangeProof{
					RangeProof: rangeProof.ToProto(),
				},
			})
			require.NoError(err)

			serverResponseChan <- responseBytes

			return nil
		},
	).AnyTimes()

	return client.GetChangeProof(ctx, request, clientDB)
}

func TestGetChangeProof(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404

	serverDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(t, err)

	clientDB, err := merkledb.New(
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

	tests := map[string]struct {
		db                        DB
		request                   *pb.SyncGetChangeProofRequest
		modifyChangeProofResponse func(*merkledb.ChangeProof)
		modifyRangeProofResponse  func(*merkledb.RangeProof)
		expectedErr               error
		expectedResponseLen       int
		expectRangeProof          bool // Otherwise expect change proof
	}{
		"proof restricted by BytesLimit": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    10000,
			},
		},
		"full response for small (single request) trie": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		"too many keys in response": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			modifyChangeProofResponse: func(response *merkledb.ChangeProof) {
				response.KeyChanges = append(response.KeyChanges, make([]merkledb.KeyChange, defaultRequestKeyLimit)...)
			},
			expectedErr: errTooManyKeys,
		},
		"partial response to request for entire trie (full leaf limit)": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		"removed first key in response": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			modifyChangeProofResponse: func(response *merkledb.ChangeProof) {
				response.KeyChanges = response.KeyChanges[1:]
			},
			expectedErr: errInvalidChangeProof,
		},
		"removed key from middle of response": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			modifyChangeProofResponse: func(response *merkledb.ChangeProof) {
				response.KeyChanges = append(response.KeyChanges[:100], response.KeyChanges[101:]...)
			},
			expectedErr: merkledb.ErrInvalidProof,
		},
		"all proof keys removed from response": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			modifyChangeProofResponse: func(response *merkledb.ChangeProof) {
				response.StartProof = nil
				response.EndProof = nil
			},
			expectedErr: merkledb.ErrInvalidProof,
		},
		"range proof response; remove first key": {
			request: &pb.SyncGetChangeProofRequest{
				// Server doesn't have the (non-existent) start root
				// so should respond with range proof.
				StartRootHash: fakeRootID[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			modifyChangeProofResponse: nil,
			modifyRangeProofResponse: func(response *merkledb.RangeProof) {
				response.KeyValues = response.KeyValues[1:]
			},
			expectedErr:      errInvalidRangeProof,
			expectRangeProof: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			// Ensure test is well-formed.
			if test.expectRangeProof {
				require.Nil(test.modifyChangeProofResponse)
			} else {
				require.Nil(test.modifyRangeProofResponse)
			}

			changeOrRangeProof, err := sendChangeProofRequest(
				t,
				serverDB,
				clientDB,
				test.request,
				1,
				test.modifyChangeProofResponse,
				test.modifyRangeProofResponse,
			)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}

			if test.expectRangeProof {
				require.NotNil(changeOrRangeProof.RangeProof)
				require.Nil(changeOrRangeProof.ChangeProof)
			} else {
				require.NotNil(changeOrRangeProof.ChangeProof)
				require.Nil(changeOrRangeProof.RangeProof)
			}

			if test.expectedResponseLen > 0 {
				if test.expectRangeProof {
					require.LessOrEqual(len(changeOrRangeProof.RangeProof.KeyValues), test.expectedResponseLen)
				} else {
					require.LessOrEqual(len(changeOrRangeProof.ChangeProof.KeyChanges), test.expectedResponseLen)
				}
			}

			var bytes []byte
			if test.expectRangeProof {
				bytes, err = proto.Marshal(&pb.SyncGetChangeProofResponse{
					Response: &pb.SyncGetChangeProofResponse_RangeProof{
						RangeProof: changeOrRangeProof.RangeProof.ToProto(),
					},
				})
			} else {
				bytes, err = proto.Marshal(&pb.SyncGetChangeProofResponse{
					Response: &pb.SyncGetChangeProofResponse_ChangeProof{
						ChangeProof: changeOrRangeProof.ChangeProof.ToProto(),
					},
				})
			}
			require.NoError(err)
			require.LessOrEqual(len(bytes), int(test.request.BytesLimit))
		})
	}
}

func TestRangeProofRetries(t *testing.T) {
	now := time.Now().UnixNano()
	t.Logf("seed: %d", now)
	r := rand.New(rand.NewSource(now)) // #nosec G404
	require := require.New(t)

	keyCount := defaultRequestKeyLimit
	db, _, err := generateTrieWithMinKeyLen(t, r, keyCount, 1)
	require.NoError(err)
	root, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	maxRequests := 4
	request := &pb.SyncGetRangeProofRequest{
		RootHash:   root[:],
		KeyLimit:   uint32(keyCount),
		BytesLimit: defaultRequestByteSizeLimit,
	}

	responseCount := 0
	modifyResponse := func(response *merkledb.RangeProof) {
		responseCount++
		if responseCount < maxRequests {
			// corrupt the first [maxRequests] responses, to force the client to retry.
			response.KeyValues = nil
		}
	}
	proof, err := sendRangeProofRequest(t, db, request, maxRequests, modifyResponse)
	require.NoError(err)
	require.Len(proof.KeyValues, keyCount)

	require.Equal(responseCount, maxRequests) // check the client performed retries.
}

// Test that a failure to send an AppRequest is propagated
// and returned by GetRangeProof and GetChangeProof.
func TestAppRequestSendFailed(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	networkClient := NewMockNetworkClient(ctrl)

	client, err := NewClient(
		&ClientConfig{
			NetworkClient: networkClient,
			Log:           logging.NoLog{},
			Metrics:       &mockMetrics{},
			BranchFactor:  merkledb.BranchFactor16,
		},
	)
	require.NoError(err)

	// Mock failure to send app request
	networkClient.EXPECT().RequestAny(
		gomock.Any(),
		gomock.Any(),
		gomock.Any(),
	).Return(ids.EmptyNodeID, nil, errAppSendFailed).Times(2)

	_, err = client.GetChangeProof(
		context.Background(),
		&pb.SyncGetChangeProofRequest{},
		nil, // database is unused
	)
	require.ErrorIs(err, errAppSendFailed)

	_, err = client.GetRangeProof(
		context.Background(),
		&pb.SyncGetRangeProofRequest{},
	)
	require.ErrorIs(err, errAppSendFailed)
}
