// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"fmt"
	"os"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/reexecute/utils"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

var _ = e2e.DescribeCChain("[Fetch Blocks]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("fetches created blocks", func() {
		env := e2e.GetEnv(tc)
		nodeURI := env.GetRandomNodeURI()
		cChainNodeURI := fmt.Sprintf("%s/ext/bc/C/rpc", nodeURI)

		fetchBlocksDBDir, err := os.MkdirTemp("", "fetch-blocks-test")
		require.NoError(err)
		defer func() {
			require.NoError(os.RemoveAll(fetchBlocksDBDir))
		}()
		const (
			startBlock = 1
			endBlock   = 10
			numBlocks  = 10
		)
		byAdvancingCChainHeight(tc, numBlocks)
		ginkgo.By("fetching blocks", func() {
			require.NoError(utils.FetchBlocksToBlockDB(
				tc.DefaultContext(),
				tc.Log(),
				fetchBlocksDBDir,
				startBlock,
				endBlock,
				cChainNodeURI,
				numBlocks,
			))
		})
		ginkgo.By("checking blockDB contents", func() {
			blockDB, err := utils.NewBlockDB(fetchBlocksDBDir)
			require.NoError(err)

			defer func() {
				require.NoError(blockDB.Close())
			}()

			blockIter := blockDB.NewIteratorFromHeight(startBlock)

			expectedBlock := startBlock
			for blockIter.Next() {
				blockHeightKey := blockIter.Key()
				blockBytes := blockIter.Value()

				require.Len(blockHeightKey, database.Uint64Size)
				blockHeight, err := database.ParseUInt64(blockHeightKey)
				require.NoError(err)
				require.Equal(expectedBlock, blockHeight)
				expectedBlock++

				block := &types.Block{}
				require.NoError(rlp.DecodeBytes(blockBytes, block))
				require.Equal(blockHeight, block.NumberU64())
			}
			require.Equal(expectedBlock, endBlock+1)
			require.NoError(blockIter.Error())
		})
	})
})
