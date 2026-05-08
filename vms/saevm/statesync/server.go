// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/saevm/saedb"
	"github.com/ava-labs/libevm/core/rawdb"
)

// GetLastStateSummary implements [adaptor.SyncVM].
func (vm *VM[_]) GetLastStateSummary(ctx context.Context) (*Summary, error) {
	lastHdr := rawdb.ReadHeadHeader(vm.db)
	if lastHdr == nil {
		return nil, errors.New("no head header found")
	}

	// We must know the settled state for the last accepted height, otherwise verification would have failed.
	height := saedb.LastCommittedTrieDBHeight(lastHdr.Number.Uint64(), vm.cfg.Config.DBConfig.CommitInterval())
	return vm.GetStateSummary(ctx, height)
}

// GetStateSummary implements [adaptor.SyncVM].
func (vm *VM[_]) GetStateSummary(ctx context.Context, height uint64) (*Summary, error) {
	hash := rawdb.ReadCanonicalHash(vm.db, height)
	b := rawdb.ReadBlock(vm.db, hash, height)
	if b == nil {
		return nil, fmt.Errorf("%w: block at height %d", database.ErrNotFound, height)
	}

	return &Summary{
		block: b,
	}, nil
}

func (vm *VM[_]) initSyncServer() {
	// TODO: start all [p2p.Handler] to serve state sync requests
	// requires #5352 at minimum

	// for hashdb, just like coreth
	// for firewood, not trivial - need vm.VM.exec.Tracker.cache.TrieDB().Backend().(*firewood.TrieDB).Firewood
	// ignoring export issues...
	handlers := make(map[uint64]p2p.Handler)
	for id, h := range handlers {
		vm.Network.AddHandler(id, h)
	}
}
