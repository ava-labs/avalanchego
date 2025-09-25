// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/precompile/precompileconfig"
)

func TestHandlePrecompileAccept(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	db := rawdb.NewMemoryDatabase()
	vm := &VM{
		chaindb:     db,
		chainConfig: params.TestChainConfig,
	}

	precompileAddr := common.Address{0x05}
	otherAddr := common.Address{0x06}

	// Prepare a receipt with 3 logs, two of which are from the precompile
	receipt := &types.Receipt{
		Logs: []*types.Log{
			{
				Address: precompileAddr,
				Topics:  []common.Hash{{0x01}, {0x02}, {0x03}},
				Data:    []byte("log1"),
			},
			{
				Address: otherAddr,
				Topics:  []common.Hash{{0x01}, {0x02}, {0x04}},
				Data:    []byte("log2"),
			},
			{
				Address: precompileAddr,
				Topics:  []common.Hash{{0x01}, {0x02}, {0x05}},
				Data:    []byte("log3"),
			},
		},
	}
	ethBlock := types.NewBlock(
		&types.Header{Number: big.NewInt(1)},
		[]*types.Transaction{types.NewTx(&types.LegacyTx{})},
		nil,
		[]*types.Receipt{receipt},
		trie.NewStackTrie(nil),
	)
	// Write the block to the db
	rawdb.WriteBlock(db, ethBlock)
	rawdb.WriteReceipts(db, ethBlock.Hash(), ethBlock.NumberU64(), []*types.Receipt{receipt})

	// Set up the mock with the expected calls to Accept
	txIndex := 0
	mockAccepter := precompileconfig.NewMockAccepter(ctrl)
	gomock.InOrder(
		mockAccepter.EXPECT().Accept(
			gomock.Not(gomock.Nil()),                // acceptCtx
			ethBlock.Hash(),                         // blockHash
			ethBlock.NumberU64(),                    // blockNumber
			ethBlock.Transactions()[txIndex].Hash(), // txHash
			0,                                       // logIndex
			receipt.Logs[0].Topics,                  // topics
			receipt.Logs[0].Data,                    // logData
		),
		mockAccepter.EXPECT().Accept(
			gomock.Not(gomock.Nil()),                // acceptCtx
			ethBlock.Hash(),                         // blockHash
			ethBlock.NumberU64(),                    // blockNumber
			ethBlock.Transactions()[txIndex].Hash(), // txHash
			2,                                       // logIndex
			receipt.Logs[2].Topics,                  // topics
			receipt.Logs[2].Data,                    // logData
		),
	)

	// Call handlePrecompileAccept
	blk, err := wrapBlock(ethBlock, vm)
	require.NoError(err)
	rules := extras.Rules{
		AccepterPrecompiles: map[common.Address]precompileconfig.Accepter{
			precompileAddr: mockAccepter,
		},
	}
	require.NoError(blk.handlePrecompileAccept(rules))
}
