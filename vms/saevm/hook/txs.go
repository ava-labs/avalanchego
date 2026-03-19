// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/strevm/blocks"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/tx"
)

func ancestorUTXOIDs(header *types.Header, settledHash common.Hash, getBlock blocks.EthBlockSource) (set.Set[ids.ID], error) {
	var consumedUTXOs set.Set[ids.ID]
	for header.ParentHash != settledHash {
		blockNumber := header.Number.Uint64() - 1
		block, ok := getBlock(header.ParentHash, blockNumber)
		if !ok {
			return nil, fmt.Errorf("missing block %s (%d)", header.ParentHash, blockNumber)
		}

		txs, err := tx.ParseSlice(customtypes.BlockExtData(block))
		if err != nil {
			return nil, fmt.Errorf("failed to extract txs of block %s (%d): %v", block.Hash(), block.NumberU64(), err)
		}

		for _, tx := range txs {
			consumedUTXOs.Union(tx.InputUTXOs())
		}
		header = block.Header()
	}
	return consumedUTXOs, nil
}
