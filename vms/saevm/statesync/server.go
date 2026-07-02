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

// RegisterServer registers the handlers for the state sync protocol.
//
// TODO(alarso16): Find a way to wire through Firewood.
func (h *SummaryHandler) RegisterServer(tdb *triedb.Database) error {
	var (
		log    = h.snowCtx.Log
		p2pNet = h.network.Network
		db     = h.db
	)
	if err := block.RegisterHandler(log, p2pNet, &blockProvider{db}); err != nil {
		return fmt.Errorf("registering block handler: %w", err)
	}

	// TODO(alarso16): Get snapshot.
	if err := hashdb.RegisterHandler(log, p2pNet, tdb, common.HashLength, p2p.EVMLeafRequestHandlerID); err != nil {
		return fmt.Errorf("registering hashdb handler: %w", err)
	}

	if err := code.RegisterHandler(log, p2pNet, db); err != nil {
		return fmt.Errorf("registering code handler: %w", err)
	}

	return nil
}

var _ block.Provider = (*blockProvider)(nil)

// blockProvider is used to serve blocks for peers during state sync.
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
