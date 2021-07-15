// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
)

var (
	chainStatePrefix  = []byte("chain")
	blockStatePrefix  = []byte("block")
	optionStatePrefix = []byte("option")
)

type State interface {
	ChainState
	BlockState
	OptionState
	WipeCache() // useful for UTs
}

type state struct {
	ChainState
	BlockState
	OptionState
}

func (s *state) WipeCache() {
	s.BlockState.WipeCache()
	s.ChainState.WipeCache()
}

func New(db database.Database) State {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	optionDB := prefixdb.New(optionStatePrefix, db)

	return &state{
		ChainState:  NewChainState(chainDB),
		BlockState:  NewBlockState(blockDB),
		OptionState: NewOptionState(optionDB),
	}
}

func NewMetered(db database.Database, namespace string, metrics prometheus.Registerer) (State, error) {
	chainDB := prefixdb.New(chainStatePrefix, db)
	blockDB := prefixdb.New(blockStatePrefix, db)
	blockState, err := NewMeteredBlockState(blockDB, namespace, metrics)
	if err != nil {
		return nil, err
	}
	optionDB := prefixdb.New(optionStatePrefix, db)
	optionState, err := NewMeteredOptionState(optionDB, namespace, metrics)
	if err != nil {
		return nil, err
	}

	return &state{
		ChainState:  NewChainState(chainDB),
		BlockState:  blockState,
		OptionState: optionState,
	}, nil
}
