// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/database/prefixdb"
	"github.com/chain4travel/caminogo/database/versiondb"
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

func New(db *versiondb.Database) State {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	heightDB := prefixdb.New(heightIndexPrefix, db)

	return &state{
		ChainState:  NewChainState(chainDB),
		BlockState:  NewBlockState(blockDB),
		HeightIndex: NewHeightIndex(heightDB, db),
	}
}

func NewMetered(db *versiondb.Database, namespace string, metrics prometheus.Registerer) (State, error) {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	heightDB := prefixdb.New(heightIndexPrefix, db)

	blockState, err := NewMeteredBlockState(blockDB, namespace, metrics)
	if err != nil {
		return nil, err
	}

	return &state{
		ChainState:  NewChainState(chainDB),
		BlockState:  blockState,
		HeightIndex: NewHeightIndex(heightDB, db),
	}, nil
}
