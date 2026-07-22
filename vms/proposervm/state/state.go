// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
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
	ChainState
	BlockState
	HeightIndex
}

func New(db *versiondb.Database, getInnerBytes func(ids.ID) ([]byte, error), log logging.Logger) State {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	heightDB := prefixdb.New(heightIndexPrefix, db)

	return &state{
		ChainState:  NewChainState(chainDB),
		BlockState:  NewBlockState(blockDB, getInnerBytes, log),
		HeightIndex: NewHeightIndex(heightDB, db),
	}
}

func NewMetered(db *versiondb.Database, namespace string, metrics prometheus.Registerer, getInnerBytes func(ids.ID) ([]byte, error), log logging.Logger) (State, error) {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	heightDB := prefixdb.New(heightIndexPrefix, db)

	blockState, err := NewMeteredBlockState(blockDB, namespace, metrics, getInnerBytes, log)
	if err != nil {
		return nil, err
	}

	return &state{
		ChainState:  NewChainState(chainDB),
		BlockState:  blockState,
		HeightIndex: NewHeightIndex(heightDB, db),
	}, nil
}
