// (c) 2021-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/x/merkledb"
)

func sendRequest(
	t *testing.T,
	db *merkledb.Database,
	request *RangeProofRequest,
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
	err := networkClient.Connected(context.Background(), serverNodeID, version.CurrentApp)
	require.NoError(err)
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
				err := handler.AppRequest(ctx, clientNodeID, requestID, deadline, requestBytes)
				require.NoError(err)
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
			response := &merkledb.RangeProof{}
			_, err := merkledb.Codec.DecodeRangeProof(responseBytes, response)
			require.NoError(err)

			// modify if needed
			if modifyResponse != nil {
				modifyResponse(response)
			}

			// reserialize the response and pass it to the client to complete the handling.
			responseBytes, err = merkledb.Codec.EncodeRangeProof(merkledb.Version, response)
			require.NoError(err)
			err = networkClient.AppResponse(context.Background(), serverNodeID, requestID, responseBytes)
			require.NoError(err)
			return nil
		},
	).AnyTimes()

	return client.GetRangeProof(ctx, request)
}

func TestGetRangeProof(t *testing.T) {
	r := rand.New(rand.NewSource(1)) // #nosec G404

	smallTrieKeyCount := defaultLeafRequestLimit
	smallTrieDB, _, err := generateTrieWithMinKeyLen(t, r, smallTrieKeyCount, 1)
	require.NoError(t, err)
	smallTrieRoot, err := smallTrieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	largeTrieKeyCount := 10_000
	largeTrieDB, largeTrieKeys, err := generateTrieWithMinKeyLen(t, r, largeTrieKeyCount, 1)
	require.NoError(t, err)
	largeTrieRoot, err := largeTrieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	tests := map[string]struct {
		db                  *merkledb.Database
		request             *RangeProofRequest
		modifyResponse      func(*merkledb.RangeProof)
		expectedErr         error
		expectedResponseLen int
	}{
		"full response for small (single request) trie": {
			db: smallTrieDB,
			request: &RangeProofRequest{
				Root:  smallTrieRoot,
				Limit: defaultLeafRequestLimit,
			},
			expectedResponseLen: defaultLeafRequestLimit,
		},
		"too many leaves in response": {
			db: smallTrieDB,
			request: &RangeProofRequest{
				Root:  smallTrieRoot,
				Limit: defaultLeafRequestLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.KeyValues = append(response.KeyValues, merkledb.KeyValue{})
			},
			expectedErr: errTooManyLeaves,
		},
		"partial response to request for entire trie (full leaf limit)": {
			db: largeTrieDB,
			request: &RangeProofRequest{
				Root:  largeTrieRoot,
				Limit: defaultLeafRequestLimit,
			},
			expectedResponseLen: defaultLeafRequestLimit,
		},
		"full response from near end of trie to end of trie (less than leaf limit)": {
			db: largeTrieDB,
			request: &RangeProofRequest{
				Root:  largeTrieRoot,
				Start: largeTrieKeys[len(largeTrieKeys)-30], // Set start 30 keys from the end of the large trie
				Limit: defaultLeafRequestLimit,
			},
			expectedResponseLen: 30,
		},
		"full response for intermediate range of trie (less than leaf limit)": {
			db: largeTrieDB,
			request: &RangeProofRequest{
				Root:  largeTrieRoot,
				Start: largeTrieKeys[1000], // Set the range for 1000 leafs in an intermediate range of the trie
				End:   largeTrieKeys[1099], // (inclusive range)
				Limit: defaultLeafRequestLimit,
			},
			expectedResponseLen: 100,
		},
		"removed first key in response": {
			db: largeTrieDB,
			request: &RangeProofRequest{
				Root:  largeTrieRoot,
				Limit: defaultLeafRequestLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.KeyValues = response.KeyValues[1:]
			},
			expectedErr: merkledb.ErrInvalidProof,
		},
		"removed first key in response and replaced proof": {
			db: largeTrieDB,
			request: &RangeProofRequest{
				Root:  largeTrieRoot,
				Limit: defaultLeafRequestLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				start := response.KeyValues[1].Key
				proof, err := largeTrieDB.GetRangeProof(context.Background(), start, nil, defaultLeafRequestLimit)
				if err != nil {
					panic(err)
				}
				response.KeyValues = proof.KeyValues
				response.StartProof = proof.StartProof
				response.EndProof = proof.EndProof
			},
			expectedErr: merkledb.ErrProofNodeNotForKey,
		},
		"removed last key in response": {
			db: largeTrieDB,
			request: &RangeProofRequest{
				Root:  largeTrieRoot,
				Limit: defaultLeafRequestLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.KeyValues = response.KeyValues[:len(response.KeyValues)-2]
			},
			expectedErr: merkledb.ErrInvalidProof,
		},
		"removed key from middle of response": {
			db: largeTrieDB,
			request: &RangeProofRequest{
				Root:  largeTrieRoot,
				Limit: defaultLeafRequestLimit,
			},
			modifyResponse: func(response *merkledb.RangeProof) {
				response.KeyValues = append(response.KeyValues[:100], response.KeyValues[101:]...)
			},
			expectedErr: merkledb.ErrInvalidProof,
		},
		"all proof keys removed from response": {
			db: largeTrieDB,
			request: &RangeProofRequest{
				Root:  largeTrieRoot,
				Limit: defaultLeafRequestLimit,
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
			proof, err := sendRequest(t, test.db, test.request, 1, test.modifyResponse)
			if test.expectedErr != nil {
				require.ErrorIs(err, test.expectedErr)
				return
			}
			require.NoError(err)
			require.Len(proof.KeyValues, test.expectedResponseLen)
		})
	}
}

func TestRetries(t *testing.T) {
	r := rand.New(rand.NewSource(1)) // #nosec G404
	require := require.New(t)

	keyCount := defaultLeafRequestLimit
	db, _, err := generateTrieWithMinKeyLen(t, r, keyCount, 1)
	require.NoError(err)
	root, err := db.GetMerkleRoot(context.Background())
	require.NoError(err)

	maxRequests := 4
	request := &RangeProofRequest{
		Root:  root,
		Limit: uint16(keyCount),
	}

	responseCount := 0
	modifyResponse := func(response *merkledb.RangeProof) {
		responseCount++
		if responseCount < maxRequests {
			// corrupt the first [maxRequests] responses, to force the client to retry.
			response.KeyValues = nil
		}
	}
	proof, err := sendRequest(t, db, request, uint32(maxRequests), modifyResponse)
	require.NoError(err)
	require.Len(proof.KeyValues, keyCount)

	require.Equal(responseCount, maxRequests) // check the client performed retries.
}
