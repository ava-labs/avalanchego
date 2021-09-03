// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
)

var (
	chainStatePrefix = []byte("chain")
	blockStatePrefix = []byte("block")
)

type State interface {
	ChainState
	BlockState
}

type state struct {
	ChainState
	BlockState
}

func (s *state) WipeCache() {
	s.ChainState.WipeCache()
	s.BlockState.WipeCache()
}

func New(db database.Database) State {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	return &state{
		ChainState: NewChainState(chainDB),
		BlockState: NewBlockState(blockDB),
	}
}

func NewMetered(db database.Database, namespace string, metrics prometheus.Registerer) (State, error) {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	blockState, err := NewMeteredBlockState(blockDB, namespace, metrics)
	if err != nil {
		return nil, err
	}

	return &state{
		ChainState: NewChainState(chainDB),
		BlockState: blockState,
	}, nil
}
