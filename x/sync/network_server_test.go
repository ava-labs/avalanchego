// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sync

import (
	"context"
	"math/rand"
	"testing"

	merkledb "github.com/avalanchego/x/merkledb"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"
)

func Test_Server_GetRangeProof(t *testing.T) {
	r := rand.New(rand.NewSource(1)) // #nosec G404

	smallTrieDB, _, err := generateTrieWithMinKeyLen(t, r, defaultRequestKeyLimit, 1)
	require.NoError(t, err)
	smallTrieRoot, err := smallTrieDB.GetMerkleRoot(context.Background())
	require.NoError(t, err)

	tests := map[string]struct {
		request             *RangeProofRequest
		modifyResponse      func(*merkledb.RangeProof)
		expectedErr         error
		expectedResponseLen int
		nodeID              ids.NodeID
		proofNil            bool
	}{
		"proof too small": {
			request: &RangeProofRequest{
				Root:       smallTrieRoot,
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 1000,
			},
			proofNil: true,
		},
		"byteslimit is 0": {
			request: &RangeProofRequest{
				Root:       smallTrieRoot,
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 0,
			},
			proofNil: true,
		},
		"keylimit is 0": {
			request: &RangeProofRequest{
				Root:       smallTrieRoot,
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 0,
			},
			proofNil: true,
		},
		"keys out of order": {
			request: &RangeProofRequest{
				Root:       smallTrieRoot,
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: constants.DefaultMaxMessageSize,
				Start:      []byte{1},
				End:        []byte{0},
			},
			proofNil: true,
		},
		"key limit too large": {
			request: &RangeProofRequest{
				Root:       smallTrieRoot,
				KeyLimit:   2 * defaultRequestKeyLimit,
				BytesLimit: constants.DefaultMaxMessageSize,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		"bytes limit too large": {
			request: &RangeProofRequest{
				Root:       smallTrieRoot,
				KeyLimit:   defaultRequestKeyLimit,
				BytesLimit: 2 * constants.DefaultMaxMessageSize,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			sender := common.NewMockSender(ctrl)
			var proofResult *merkledb.RangeProof
			sender.EXPECT().SendAppResponse(
				gomock.Any(), // ctx
				gomock.Any(),
				gomock.Any(), // requestID
				gomock.Any(), // responseBytes
			).DoAndReturn(
				func(_ context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
					// deserialize the response so we can modify it if needed.
					if !test.proofNil {
						var err error
						proofResult = &merkledb.RangeProof{}
						_, err = merkledb.Codec.DecodeRangeProof(responseBytes, proofResult)
						require.NoError(err)
					}
					return nil
				},
			).AnyTimes()
			handler := NewNetworkServer(sender, smallTrieDB, logging.NoLog{})
			err := handler.HandleRangeProofRequest(context.Background(), test.nodeID, 0, test.request)
			if test.expectedErr != nil {
				require.ErrorIs(err, test.expectedErr)
				return
			}
			require.NoError(err)
			if test.proofNil {
				require.Nil(proofResult)
				return
			}
			require.NotNil(proofResult)
			if test.expectedResponseLen > 0 {
				require.LessOrEqual(len(proofResult.KeyValues), test.expectedResponseLen)
			}

			bytes, err := merkledb.Codec.EncodeRangeProof(Version, proofResult)
			require.NoError(err)
			require.Less(len(bytes), int(test.request.BytesLimit))
		})
	}
}

func Test_Server_GetChangeProof(t *testing.T) {
	require := require.New(t)

	r := rand.New(rand.NewSource(1)) // #nosec G404
	trieDB, _, err := generateTrieWithMinKeyLen(t, r, defaultRequestKeyLimit, 1)
	require.NoError(err)

	startRoot, err := trieDB.GetMerkleRoot(context.Background())
	require.NoError(err)

	// create changes
	for x := 0; x < 600; x++ {
		view, err := trieDB.NewView()
		require.NoError(err)

		key := make([]byte, r.Intn(100))
		_, err = r.Read(key)
		require.NoError(err)

		val := make([]byte, r.Intn(100))
		_, err = r.Read(val)
		require.NoError(err)

		err = view.Insert(context.Background(), key, val)
		require.NoError(err)

		deleteKeyStart := make([]byte, r.Intn(10))
		_, err = r.Read(deleteKeyStart)
		require.NoError(err)

		it := trieDB.NewIteratorWithStart(deleteKeyStart)
		if it.Next() {
			err = view.Remove(context.Background(), it.Key())
			require.NoError(err)
		}
		require.NoError(it.Error())
		it.Release()

		view.CommitToDB(context.Background())
	}

	endRoot, err := trieDB.GetMerkleRoot(context.Background())
	require.NoError(err)

	tests := map[string]struct {
		request             *ChangeProofRequest
		modifyResponse      func(proof *merkledb.ChangeProof)
		expectedErr         error
		expectedResponseLen int
		nodeID              ids.NodeID
		proofNil            bool
	}{
		"proof too small": {
			request: &ChangeProofRequest{
				StartingRoot: startRoot,
				EndingRoot:   endRoot,
				KeyLimit:     defaultRequestKeyLimit,
				BytesLimit:   1000,
			},
			proofNil: true,
		},
		"byteslimit is 0": {
			request: &ChangeProofRequest{
				StartingRoot: startRoot,
				EndingRoot:   endRoot,
				KeyLimit:     defaultRequestKeyLimit,
				BytesLimit:   0,
			},
			proofNil: true,
		},
		"keylimit is 0": {
			request: &ChangeProofRequest{
				StartingRoot: startRoot,
				EndingRoot:   endRoot,
				KeyLimit:     defaultRequestKeyLimit,
				BytesLimit:   0,
			},
			proofNil: true,
		},
		"keys out of order": {
			request: &ChangeProofRequest{
				StartingRoot: startRoot,
				EndingRoot:   endRoot,
				KeyLimit:     defaultRequestKeyLimit,
				BytesLimit:   constants.DefaultMaxMessageSize,
				Start:        []byte{1},
				End:          []byte{0},
			},
			proofNil: true,
		},
		"key limit too large": {
			request: &ChangeProofRequest{
				StartingRoot: startRoot,
				EndingRoot:   endRoot,
				KeyLimit:     2 * defaultRequestKeyLimit,
				BytesLimit:   constants.DefaultMaxMessageSize,
			},
			expectedResponseLen: defaultRequestKeyLimit,
		},
		"bytes limit too large": {
			request: &ChangeProofRequest{
				StartingRoot: startRoot,
				EndingRoot:   endRoot,
				KeyLimit:     defaultRequestKeyLimit,
				BytesLimit:   2 * constants.DefaultMaxMessageSize,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			sender := common.NewMockSender(ctrl)
			var proofResult *merkledb.ChangeProof
			sender.EXPECT().SendAppResponse(
				gomock.Any(), // ctx
				gomock.Any(),
				gomock.Any(), // requestID
				gomock.Any(), // responseBytes
			).DoAndReturn(
				func(_ context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
					// deserialize the response so we can modify it if needed.
					if !test.proofNil {
						var err error
						proofResult = &merkledb.ChangeProof{}
						_, err = merkledb.Codec.DecodeChangeProof(responseBytes, proofResult)
						require.NoError(err)
					}
					return nil
				},
			).AnyTimes()
			handler := NewNetworkServer(sender, trieDB, logging.NoLog{})
			err := handler.HandleChangeProofRequest(context.Background(), test.nodeID, 0, test.request)
			if test.expectedErr != nil {
				require.ErrorIs(err, test.expectedErr)
				return
			}
			require.NoError(err)
			if test.proofNil {
				require.Nil(proofResult)
				return
			}
			require.NotNil(proofResult)
			if test.expectedResponseLen > 0 {
				require.LessOrEqual(len(proofResult.KeyValues)+len(proofResult.DeletedKeys), test.expectedResponseLen)
			}

			bytes, err := merkledb.Codec.EncodeChangeProof(Version, proofResult)
			require.NoError(err)
			require.Less(len(bytes), int(test.request.BytesLimit))
		})
	}
}
