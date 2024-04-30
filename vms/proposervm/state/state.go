// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	chainStatePrefix  = []byte("chain")
	blockStatePrefix  = []byte("block")
	heightIndexPrefix = []byte("height")
)

type State interface {
	ChainState
	BlockState
	HeightIndex
}

type state struct {
	*chainState
	BlockState
	HeightIndex
}

func New(db *versiondb.Database) (State, error) {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	heightDB := prefixdb.New(heightIndexPrefix, db)

	s := &state{
		chainState:  newChainState(chainDB),
		BlockState:  NewBlockState(blockDB),
		HeightIndex: NewHeightIndex(heightDB, db),
	}

	return s, s.pruneVerifiedBlocks(db)
}

func NewMetered(db *versiondb.Database, namespace string, metrics prometheus.Registerer) (State, error) {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	heightDB := prefixdb.New(heightIndexPrefix, db)

	blockState, err := NewMeteredBlockState(blockDB, namespace, metrics)
	if err != nil {
		return nil, err
	}

	s := &state{
		chainState:  newChainState(chainDB),
		BlockState:  blockState,
		HeightIndex: NewHeightIndex(heightDB, db),
	}

	return s, s.pruneVerifiedBlocks(db)
}

func (s *state) pruneVerifiedBlocks(db *versiondb.Database) error {
	preferredID, err := s.chainState.GetPreference()
	switch {
	case err == database.ErrNotFound:
		return nil
	case err != nil:
		return err
	}

	// Prune all verified blocks that are not in the preferred chain
	preferredBlkIDs := set.Set[ids.ID]{}
	preferredBlk, status, err := s.BlockState.GetBlock(preferredID)
	if err != nil {
		return nil
	}

	for status == choices.Processing {
		preferredBlkIDs.Add(preferredBlk.ID())
		preferredBlk, status, err = s.BlockState.GetBlock(preferredBlk.ParentID())
		if err != nil {
			return nil
		}
	}

	iter := s.chainState.db.NewIteratorWithPrefix([]byte{verifiedByte})
	defer iter.Release()

	for iter.Next() {
		blkID := ids.ID(iter.Key()[1:]) // Drop the one byte prefix
		if preferredBlkIDs.Contains(blkID) {
			continue
		}

		if err := s.chainState.DeleteProcessingBlock(blkID); err != nil {
			return nil
		}

		if err := s.BlockState.DeleteBlock(blkID); err != nil {
			return nil
		}
	}

	if err := iter.Error(); err != nil {
		return err
	}

	return db.Commit()
}
