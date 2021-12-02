// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
)

var (
	chainStatePrefix         = []byte("chain")
	blockStatePrefix         = []byte("block")
	innerBlocksMappingPrefix = []byte("mapping")
)

type State interface {
	ChainState
	BlockState
	InnerBlocksMapping
}

type state struct {
	ChainState
	BlockState
	InnerBlocksMapping
}

func New(db database.Database) State {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	mappingDB := prefixdb.New(innerBlocksMappingPrefix, db)
	return &state{
		ChainState:         NewChainState(chainDB),
		BlockState:         NewBlockState(blockDB),
		InnerBlocksMapping: NewInnerBlocksMapping(mappingDB),
	}
}

func NewMetered(db database.Database, namespace string, metrics prometheus.Registerer) (State, error) {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	mappingDB := prefixdb.New(innerBlocksMappingPrefix, db)

	blockState, err := NewMeteredBlockState(blockDB, namespace, metrics)
	if err != nil {
		return nil, err
	}

	return &state{
		ChainState:         NewChainState(chainDB),
		BlockState:         blockState,
		InnerBlocksMapping: NewInnerBlocksMapping(mappingDB),
	}, nil
}

func (s *state) clearCache() {
	s.ChainState.clearCache()
	s.BlockState.clearCache()
	s.InnerBlocksMapping.clearCache()
}
