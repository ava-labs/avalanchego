// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hook

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/corethvm/tx"

	saetypes "github.com/ava-labs/strevm/types"
)

func ancestorUTXOIDs(header *types.Header, settledHash common.Hash, source saetypes.BlockSource) (set.Set[ids.ID], error) {
	var consumedUTXOs set.Set[ids.ID]
	for header.ParentHash != settledHash {
		parentNumber := header.Number.Uint64() - 1
		parent, ok := source(header.ParentHash, parentNumber)
		if !ok {
			return nil, fmt.Errorf("missing block %s (%d)", header.ParentHash, parentNumber)
		}

		txs, err := tx.ParseSlice(customtypes.BlockExtData(parent))
		if err != nil {
			return nil, fmt.Errorf("failed to extract txs of block %s (%d): %w", parent.Hash(), parent.NumberU64(), err)
		}

		for _, tx := range txs {
			consumedUTXOs.Union(tx.InputUTXOs())
		}
		header = parent.Header()
	}
	return consumedUTXOs, nil
}
