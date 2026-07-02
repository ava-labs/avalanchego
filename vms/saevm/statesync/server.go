// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
)

// RegisterServer registers the handlers for the state sync protocol.
//
// TODO(alarso16): Find a way to wire through Firewood.
func (h *SummaryHandler) RegisterServer(tdb *triedb.Database, snap *snapshot.Tree) error {
	var (
		log    = h.snowCtx.Log
		p2pNet = h.network.Network
		db     = h.db
	)
	if err := block.RegisterHandler(log, p2pNet, db); err != nil {
		return fmt.Errorf("registering block handler: %w", err)
	}

	if err := evmstate.RegisterHandler(
		log, p2pNet,
		p2p.EVMLeafRequestHandlerID,
		tdb,
		common.HashLength,
		evmstate.WithSnapshot(snap),
	); err != nil {
		return fmt.Errorf("registering hashdb handler: %w", err)
	}

	if err := code.RegisterHandler(log, p2pNet, db); err != nil {
		return fmt.Errorf("registering code handler: %w", err)
	}

	return nil
}
