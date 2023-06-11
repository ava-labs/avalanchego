// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/x/merkledb"

	pb "github.com/ava-labs/avalanchego/proto/pb/sync"
)

func newDefaultDBConfig() merkledb.Config {
	return merkledb.Config{
		EvictionBatchSize: 100,
		HistoryLength:     defaultRequestKeyLimit,
		NodeCacheSize:     defaultRequestKeyLimit,
		Reg:               prometheus.NewRegistry(),
		Tracer:            newNoopTracer(),
	}
}

func sendRangeRequest(
	t *testing.T,
	db SyncableDB,
	request *pb.SyncGetRangeProofRequest,
	maxAttempts uint32,
	modifyResponse func(*merkledb.RangeProof),
) (*merkledb.RangeProof, error) {
	t.Helper()

	var wg sync.WaitGroup
	defer wg.Wait() // wait for goroutines spawned

	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	sender := common.NewMockSender(ctrl)
	handler := NewNetworkServer(sender, db, logging.NoLog{})
	clientNodeID, serverNodeID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	networkClient := NewNetworkClient(sender, clientNodeID, 1, logging.NoLog{})
	require.NoError(networkClient.Connected(context.Background(), serverNodeID, version.CurrentApp))
	client := NewClient(&ClientConfig{
		NetworkClient: networkClient,
		Metrics:       &mockMetrics{},
		Log:           logging.NoLog{},
	})

	ctx, cancel := context.WithCancel(context.Background())
	deadline := time.Now().Add(1 * time.Hour) // enough time to complete a request
	defer cancel()                            // avoid leaking a goroutine

	expectedSendNodeIDs := set.NewSet[ids.NodeID](1)
	expectedSendNodeIDs.Add(serverNodeID)
	sender.EXPECT().SendAppRequest(
		gomock.Any(),        // ctx
		expectedSendNodeIDs, // {serverNodeID}
		gomock.Any(),        // requestID
		gomock.Any(),        // requestBytes
	).DoAndReturn(
		func(ctx context.Context, _ set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
			// limit the number of attempts to [maxAttempts] by cancelling the context if needed.
			if requestID >= maxAttempts {
				cancel()
				return ctx.Err()
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				require.NoError(handler.AppRequest(ctx, clientNodeID, requestID, deadline, requestBytes))
			}() // should be on a goroutine so the test can make progress.
			return nil
		},
	).AnyTimes()
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
			require.NoError(networkClient.AppResponse(context.Background(), serverNodeID, requestID, responseBytes))
			return nil
		},
	).AnyTimes()

	return client.GetRangeProof(ctx, request)
}

func TestGetRangeProof(t *testing.T) {
	r := rand.New(rand.NewSource(1)) // #nosec G404

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
		db                  SyncableDB
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
				RootHash:   largeTrieRoot[:],
				StartKey:   largeTrieKeys[len(largeTrieKeys)-30], // Set start 30 keys from the end of the large trie
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			expectedResponseLen: 30,
		},
		"full response for intermediate range of trie (less than leaf limit)": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   largeTrieRoot[:],
				StartKey:   largeTrieKeys[1000], // Set the range for 1000 leafs in an intermediate range of the trie
				EndKey:     largeTrieKeys[1099], // (inclusive range)
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
				start := response.KeyValues[1].Key
				rootID, err := largeTrieDB.GetMerkleRoot(context.Background())
				require.NoError(t, err)
				proof, err := largeTrieDB.GetRangeProofAtRoot(context.Background(), rootID, start, nil, defaultRequestKeyLimit)
				require.NoError(t, err)
				response.KeyValues = proof.KeyValues
				response.StartProof = proof.StartProof
				response.EndProof = proof.EndProof
			},
			expectedErr: merkledb.ErrProofNodeNotForKey,
		},
		"removed last key in response": {
			db: largeTrieDB,
			request: &pb.SyncGetRangeProofRequest{
				RootHash:   largeTrieRoot[:],
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.KeyValues = response.KeyValues[:len(response.KeyValues)-2]
			},
			expectedErr: merkledb.ErrProofNodeNotForKey,
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
		"all proof keys removed from response": {
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
			expectedErr: merkledb.ErrInvalidProof,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			proof, err := sendRangeRequest(t, test.db, test.request, 1, test.modifyResponse)
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

func sendChangeRequest(
	t *testing.T,
	db SyncableDB,
	verificationDB SyncableDB,
	request *pb.SyncGetChangeProofRequest,
	maxAttempts uint32,
	modifyResponse func(*merkledb.ChangeProof),
) (*merkledb.ChangeProof, error) {
	t.Helper()

	var wg sync.WaitGroup
	defer wg.Wait() // wait for goroutines spawned

	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	sender := common.NewMockSender(ctrl)
	handler := NewNetworkServer(sender, db, logging.NoLog{})
	clientNodeID, serverNodeID := ids.GenerateTestNodeID(), ids.GenerateTestNodeID()
	networkClient := NewNetworkClient(sender, clientNodeID, 1, logging.NoLog{})
	require.NoError(networkClient.Connected(context.Background(), serverNodeID, version.CurrentApp))
	client := NewClient(&ClientConfig{
		NetworkClient: networkClient,
		Metrics:       &mockMetrics{},
		Log:           logging.NoLog{},
	})

	ctx, cancel := context.WithCancel(context.Background())
	deadline := time.Now().Add(1 * time.Hour) // enough time to complete a request
	defer cancel()                            // avoid leaking a goroutine

	expectedSendNodeIDs := set.NewSet[ids.NodeID](1)
	expectedSendNodeIDs.Add(serverNodeID)
	sender.EXPECT().SendAppRequest(
		gomock.Any(),        // ctx
		expectedSendNodeIDs, // {serverNodeID}
		gomock.Any(),        // requestID
		gomock.Any(),        // requestBytes
	).DoAndReturn(
		func(ctx context.Context, _ set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
			// limit the number of attempts to [maxAttempts] by cancelling the context if needed.
			if requestID >= maxAttempts {
				cancel()
				return ctx.Err()
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				require.NoError(handler.AppRequest(ctx, clientNodeID, requestID, deadline, requestBytes))
			}() // should be on a goroutine so the test can make progress.
			return nil
		},
	).AnyTimes()
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

			var changeProof merkledb.ChangeProof
			// TODO when the client/server support including range proofs in the response,
			// this will need to be updated.
			require.NoError(changeProof.UnmarshalProto(responseProto.GetChangeProof()))

			// modify if needed
			if modifyResponse != nil {
				modifyResponse(&changeProof)
			}

			// reserialize the response and pass it to the client to complete the handling.
			responseBytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
				Response: &pb.SyncGetChangeProofResponse_ChangeProof{
					ChangeProof: changeProof.ToProto(),
				},
			})
			require.NoError(err)
			require.NoError(networkClient.AppResponse(context.Background(), serverNodeID, requestID, responseBytes))
			return nil
		},
	).AnyTimes()

	return client.GetChangeProof(ctx, request, verificationDB)
}

func TestGetChangeProof(t *testing.T) {
	r := rand.New(rand.NewSource(1)) // #nosec G404

	trieDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(t, err)

	verificationDB, err := merkledb.New(
		context.Background(),
		memdb.New(),
		newDefaultDBConfig(),
	)
	require.NoError(t, err)
	startRoot, err := trieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	// create changes
	for x := 0; x < defaultRequestKeyLimit/2; x++ {
		view, err := trieDB.NewView()
		require.NoError(t, err)

		// add some key/values
		for i := 0; i < 10; i++ {
			key := make([]byte, r.Intn(100))
			_, err = r.Read(key)
			require.NoError(t, err)

			val := make([]byte, r.Intn(100))
			_, err = r.Read(val)
			require.NoError(t, err)

			require.NoError(t, view.Insert(context.Background(), key, val))
		}

		// delete a key
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
		db                  SyncableDB
		request             *pb.SyncGetChangeProofRequest
		modifyResponse      func(*merkledb.ChangeProof)
		expectedErr         error
		expectedResponseLen int
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
			modifyResponse: func(response *merkledb.ChangeProof) {
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
			modifyResponse: func(response *merkledb.ChangeProof) {
				response.KeyChanges = response.KeyChanges[1:]
			},
			expectedErr: merkledb.ErrInvalidProof,
		},
		"removed last key in response": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.ChangeProof) {
				response.KeyChanges = response.KeyChanges[:len(response.KeyChanges)-2]
			},
			expectedErr: merkledb.ErrProofNodeNotForKey,
		},
		"removed key from middle of response": {
			request: &pb.SyncGetChangeProofRequest{
				StartRootHash: startRoot[:],
				EndRootHash:   endRoot[:],
				KeyLimit:      defaultRequestKeyLimit,
				BytesLimit:    defaultRequestByteSizeLimit,
			},
			modifyResponse: func(response *merkledb.ChangeProof) {
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
			modifyResponse: func(response *merkledb.ChangeProof) {
				response.StartProof = nil
				response.EndProof = nil
			},
			expectedErr: merkledb.ErrInvalidProof,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			proof, err := sendChangeRequest(t, trieDB, verificationDB, test.request, 1, test.modifyResponse)
			require.ErrorIs(err, test.expectedErr)
			if test.expectedErr != nil {
				return
			}
			if test.expectedResponseLen > 0 {
				require.LessOrEqual(len(proof.KeyChanges), test.expectedResponseLen)
			}

			// TODO when the client/server support including range proofs in the response,
			// this will need to be updated.
			bytes, err := proto.Marshal(&pb.SyncGetChangeProofResponse{
				Response: &pb.SyncGetChangeProofResponse_ChangeProof{
					ChangeProof: proof.ToProto(),
				},
			})
			require.NoError(err)
			require.LessOrEqual(len(bytes), int(test.request.BytesLimit))
		})
	}
}

func TestRangeProofRetries(t *testing.T) {
	r := rand.New(rand.NewSource(1)) // #nosec G404
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
	proof, err := sendRangeRequest(t, db, request, uint32(maxRequests), modifyResponse)
	require.NoError(err)
	require.Len(proof.KeyValues, keyCount)

	require.Equal(responseCount, maxRequests) // check the client performed retries.
}
