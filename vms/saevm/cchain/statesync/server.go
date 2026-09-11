// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/triedb"

	cchainstate "github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
)

// RegisterServer registers the SAE state sync handler with the given EVM trie
// database, allowing this node to server others' state sync requests. The
// atomic trie's served requests are counted under [atomicMetricsNamespace];
// the embedded handler registers its own under its namespace.
func (h *Handler) RegisterServer(tdb *triedb.Database, snaps *snapshot.Tree) error {
	if err := cchainstate.RegisterSyncHandler(h.network.Network, h.state, h.atomicReg); err != nil {
		return fmt.Errorf("registering C-Chain state handler: %w", err)
	}

	return h.Handler.RegisterServer(tdb, snaps)
}
