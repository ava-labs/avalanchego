// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
)

// RegisterServer registers the SAE state sync handler with the given EVM trie
// database, allowing this node to server others' state sync requests.
//
// TODO(alarso16): wire through snapshot.
func (h *SummaryHandler) RegisterServer(tdb *triedb.Database, snaps *snapshot.Tree) error {
	if err := state.RegisterSyncHandler(h.network.Network, h.state); err != nil {
		return fmt.Errorf("registering C-Chain state handler: %w", err)
	}

	return h.SummaryHandler.RegisterServer(tdb, snaps)
}
