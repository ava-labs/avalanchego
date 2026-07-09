// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"fmt"

	"github.com/ava-labs/libevm/core/state/snapshot"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/saevm/statesync"

	cchainstate "github.com/ava-labs/avalanchego/vms/saevm/cchain/state"
)

// RegisterServer registers the SAE state sync handler with the given EVM trie
// database, allowing this node to server others' state sync requests.
func RegisterServer(
	log logging.Logger,
	network *p2p.Network,
	db ethdb.Database,
	tdb *triedb.Database,
	snaps *snapshot.Tree,
	state *cchainstate.State,
) error {
	if err := cchainstate.RegisterSyncHandler(network, state); err != nil {
		return fmt.Errorf("registering C-Chain state handler: %w", err)
	}

	return statesync.RegisterHandlers(log, network, db, tdb, snaps)
}
