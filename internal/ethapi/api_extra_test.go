// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/subnet-evm/rpc"
)

func TestBlockChainAPI_stateQueryBlockNumberAllowed(t *testing.T) {
	t.Parallel()

	const (
		queryWindow      uint64 = 1024
		nonArchiveWindow uint64 = 32
	)

	makeBlockWithNumber := func(number uint64) *types.Block {
		header := &types.Header{
			Number: big.NewInt(int64(number)),
		}
		return types.NewBlock(header, nil, nil, nil, nil)
	}

	testCases := map[string]struct {
		blockNumOrHash rpc.BlockNumberOrHash
		makeBackend    func(ctrl *gomock.Controller) *MockBackend
		wantErrMessage string
	}{
		"zero_query_window": {
			blockNumOrHash: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(1000)),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().IsArchive().Return(true)
				backend.EXPECT().HistoricalProofQueryWindow().Return(uint64(0))
				return backend
			},
		},
		"block_number_allowed_below_window": {
			blockNumOrHash: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(1000)),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().IsArchive().Return(true)
				backend.EXPECT().HistoricalProofQueryWindow().Return(queryWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(1020))
				return backend
			},
		},
		"block_number_allowed": {
			blockNumOrHash: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(2000)),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().IsArchive().Return(true)
				backend.EXPECT().HistoricalProofQueryWindow().Return(queryWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(2200))
				return backend
			},
		},
		"block_number_allowed_by_hash": {
			blockNumOrHash: rpc.BlockNumberOrHashWithHash(common.Hash{99}, false),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().IsArchive().Return(true)
				backend.EXPECT().HistoricalProofQueryWindow().Return(queryWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(2200))
				backend.EXPECT().
					BlockByHash(gomock.Any(), gomock.Any()).
					Return(makeBlockWithNumber(2000), nil)
				return backend
			},
		},
		"block_number_allowed_by_hash_error": {
			blockNumOrHash: rpc.BlockNumberOrHashWithHash(common.Hash{99}, false),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().IsArchive().Return(true)
				backend.EXPECT().HistoricalProofQueryWindow().Return(queryWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(2200))
				backend.EXPECT().
					BlockByHash(gomock.Any(), gomock.Any()).
					Return(nil, errors.New("test error"))
				return backend
			},
			wantErrMessage: "failed to get block from hash: test error",
		},
		"block_number_out_of_window": {
			blockNumOrHash: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(1000)),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().IsArchive().Return(true)
				backend.EXPECT().HistoricalProofQueryWindow().Return(queryWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(2200))
				return backend
			},
			wantErrMessage: "block number 1000 is before the oldest allowed block number 1176 (window of 1024 blocks)",
		},
		"block_number_out_of_window_non_archive": {
			blockNumOrHash: rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(1000)),
			makeBackend: func(ctrl *gomock.Controller) *MockBackend {
				backend := NewMockBackend(ctrl)
				backend.EXPECT().IsArchive().Return(false)
				backend.EXPECT().HistoricalProofQueryWindow().Return(nonArchiveWindow)
				backend.EXPECT().LastAcceptedBlock().Return(makeBlockWithNumber(1033))
				return backend
			},
			wantErrMessage: "block number 1000 is before the oldest allowed block number 1001 (window of 32 blocks)",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)

			api := &BlockChainAPI{
				b: testCase.makeBackend(ctrl),
			}

			err := api.stateQueryBlockNumberAllowed(testCase.blockNumOrHash)
			if testCase.wantErrMessage == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, testCase.wantErrMessage)
			}
		})
	}
}
