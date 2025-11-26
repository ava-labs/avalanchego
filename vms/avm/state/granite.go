// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

var _ ChainDB = (*NoChainDB)(nil)

// NoChainDB is used before the Firewood migration when chain state was kept in
// the same database as the rest of the state.
type NoChainDB struct {
	State State
}

// AddAtomicTx is a no-op because atomic txs are not a part of chain state
func (*NoChainDB) AddAtomicTx(ids.ID) {}

// Repair is a no-op because the db is always written atomically with the
// rest of the State.
func (*NoChainDB) Repair(VM, State) error {
	return nil
}

func (*NoChainDB) Abort() {}

func (*NoChainDB) Commit() error {
	return nil
}

func (d *NoChainDB) Height() (uint64, bool) {
	return d.State.GetLastAcceptedHeight(), true
}

func (*NoChainDB) Close(context.Context) error {
	return nil
}
