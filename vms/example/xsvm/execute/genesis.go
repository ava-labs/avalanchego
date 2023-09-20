// Copyright (C) 2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package execute

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"

	"github.com/ava-labs/xsvm/block"
	"github.com/ava-labs/xsvm/genesis"
	"github.com/ava-labs/xsvm/state"
)

func Genesis(db database.KeyValueReaderWriterDeleter, chainID ids.ID, g *genesis.Genesis) error {
	isInitialized, err := state.IsInitialized(db)
	if err != nil {
		return err
	}
	if isInitialized {
		return nil
	}

	blk, err := genesis.Block(g)
	if err != nil {
		return err
	}

	for _, allocation := range g.Allocations {
		if err := state.SetBalance(db, allocation.Address, chainID, allocation.Balance); err != nil {
			return err
		}
	}

	blkID, err := blk.ID()
	if err != nil {
		return err
	}

	blkBytes, err := block.Codec.Marshal(block.Version, blk)
	if err != nil {
		return err
	}

	if err := state.AddBlock(db, blkID, blkBytes); err != nil {
		return err
	}
	if err := state.SetLastAccepted(db, blkID); err != nil {
		return err
	}
	return state.SetInitialized(db)
}
