// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ethapi

import (
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/consensus/dummy"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/rpc"

	ethparams "github.com/ava-labs/libevm/params"
)

func TestMain(m *testing.M) {
	core.RegisterExtras()
	customtypes.Register()
	params.RegisterExtras()
	os.Exit(m.Run())
}

func TestBlockchainAPI_GetChainConfig(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wantConfig := &params.ChainConfig{
		ChainID: big.NewInt(43114),
	}
	backend := NewMockBackend(ctrl)
	backend.EXPECT().ChainConfig().Return(wantConfig)

	api := NewBlockChainAPI(backend)

	gotConfig := api.GetChainConfig(t.Context())
	require.Equal(t, params.ToWithUpgradesJSON(wantConfig), gotConfig)
}

// Copy one test case from TestCall
func TestBlockchainAPI_CallDetailed(t *testing.T) {
	t.Parallel()
	// Initialize test accounts
	var (
		accounts = newAccounts(2)
		genesis  = &core.Genesis{
			Config: params.TestChainConfig,
			Alloc: types.GenesisAlloc{
				accounts[0].addr: {Balance: big.NewInt(params.Ether)},
				accounts[1].addr: {Balance: big.NewInt(params.Ether)},
			},
		}
		genBlocks   = 10
		signer      = types.HomesteadSigner{}
		blockNumber = rpc.LatestBlockNumber
	)
	api := NewBlockChainAPI(newTestBackend(t, genBlocks, genesis, dummy.NewCoinbaseFaker(), func(i int, b *core.BlockGen) {
		// Transfer from account[0] to account[1]
		//    value: 1000 wei
		//    fee:   0 wei
		tx, _ := types.SignTx(types.NewTx(&types.LegacyTx{Nonce: uint64(i), To: &accounts[1].addr, Value: big.NewInt(1000), Gas: ethparams.TxGas, GasPrice: b.BaseFee(), Data: nil}), signer, accounts[0].key)
		b.AddTx(tx)
	}))

	result, err := api.CallDetailed(
		t.Context(),
		TransactionArgs{
			From:  &accounts[0].addr,
			To:    &accounts[1].addr,
			Value: (*hexutil.Big)(big.NewInt(1000)),
		},
		rpc.BlockNumberOrHash{BlockNumber: &blockNumber},
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 0, result.ErrCode)
	require.Nil(t, result.ReturnData)
	require.Equal(t, ethparams.TxGas, result.UsedGas)
	require.Empty(t, result.Err)
}

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
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, testCase.wantErrMessage)
			}
		})
	}
}
