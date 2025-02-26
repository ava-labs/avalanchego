// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/merkledb/firewooddb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/merkledb"
)

type ChainDB interface {
	Close() error
	NewBatch() (database.Batch, error)
	AddAtomicTx(txID ids.ID)
	Abort()
}

type FirewoodChainDB struct {
	pending merkledb.Changes

	db *firewooddb.DB
}

func (f *FirewoodChainDB) Close() error {
	return f.db.Close()
}

func (f *FirewoodChainDB) NewBatch() (
	database.Batch,
	error,
) {
	return f.db.NewBatch(), nil
}

func (f *FirewoodChainDB) Abort() {
	f.pending = merkledb.Changes{}
}

func (f *FirewoodChainDB) AddAtomicTx(txID ids.ID) {
	f.pending.Append(txID[:], nil)
}
