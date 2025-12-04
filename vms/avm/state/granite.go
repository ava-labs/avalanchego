// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
)

// Deprecated: after the firewood db migration this type should no longer be
// used.
//
// NoChainDB is used before the Firewood migration when chain state was kept in
// the same database as the rest of the state.
type NoChainDB struct {
	VersionDB *versiondb.Database
}

var _ ChainDB = (*NoChainDB)(nil)

// AddAtomicTx is a no-op because atomic txs are not a part of chain state
func (*NoChainDB) AddAtomicTx(ids.ID) {}

// Repair is a no-op because the db is always written atomically with the
// rest of the State.
func (*NoChainDB) Repair(context.Context, VM, State) error {
	return nil
}

// Abort is a no-op because there is no chain-specific db.
func (*NoChainDB) Abort() {}

func (db *NoChainDB) CommitBatch(uint64) (database.Batch, error) {
	return db.VersionDB.CommitBatch()
}

func (*NoChainDB) Close(context.Context) error {
	return nil
}
