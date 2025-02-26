// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/database"
	"errors"
	"github.com/ava-labs/avalanchego/ids"
)

var _ ChainDB = (*GraniteChainDB)(nil)

type GraniteChainDB struct {
	baseDB database.Database
	vdb    *versiondb.Database
}

// Atomic txs are not a part of chain state
func (g *GraniteChainDB) AddAtomicTx(ids.ID) {}

func NewGraniteChainDB(
	baseDB database.Database,
	vdb *versiondb.Database,
) *GraniteChainDB {
	return &GraniteChainDB{
		baseDB: baseDB,
		vdb:    vdb,
	}
}

func (g *GraniteChainDB) Close() error {
	return errors.Join(
		g.vdb.Close(),
		g.baseDB.Close(),
	)
}

func (g *GraniteChainDB) NewBatch() (database.Batch, error) {
	return g.vdb.CommitBatch()
}

func (g *GraniteChainDB) Abort() {
	g.vdb.Abort()
}
