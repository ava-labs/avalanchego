// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/hashdb"
)

func (h *SummaryHandler) RegisterServer(tdb *triedb.Database) error {
	if err := block.RegisterHandler(h.snowCtx.Log, h.network.Network, &blockProvider{h.db}); err != nil {
		return fmt.Errorf("registering block handler: %w", err)
	}

	if err := hashdb.RegisterHandler(h.snowCtx.Log, h.network.Network, tdb, common.HashLength, nil, p2p.EVMLeafRequestHandlerID); err != nil {
		return fmt.Errorf("registering hashdb handler: %w", err)
	}

	if err := code.RegisterHandler(h.snowCtx.Log, h.network.Network, h.db); err != nil {
		return fmt.Errorf("registering code handler: %w", err)
	}

	return nil
}

var _ block.Provider = (*blockProvider)(nil)

// blockProvider is used to serve blocks for peers during state sync.
//
// TODO(alarso16): Should we try and find them in memory?
type blockProvider struct {
	db ethdb.Database
}

func (b *blockProvider) GetBlock(hash common.Hash, height uint64) *types.Block {
	return rawdb.ReadBlock(b.db, hash, height)
}

func (b *blockProvider) GetBlockByHeight(height uint64) *types.Block {
	hash := rawdb.ReadCanonicalHash(b.db, height)
	return b.GetBlock(hash, height)
}
