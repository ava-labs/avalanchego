// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/triedb"
	"github.com/prometheus/client_golang/prometheus"

	cchainstate "github.com/ava-labs/avalanchego/vms/evm/imp/cchain/state"
)

// RegisterHandlers registers the SAE state sync handler with the given EVM trie
// database, allowing this node to serve others' state sync requests.
func (h *Handler) RegisterHandlers(tdb *triedb.Database, snaps *snapshot.Tree, registerer prometheus.Registerer) error {
	if err := cchainstate.RegisterSyncHandler(h.network.Network, h.state); err != nil {
		return fmt.Errorf("registering C-Chain state handler: %w", err)
	}

	return h.Handler.RegisterHandlers(tdb, snaps, registerer)
}
