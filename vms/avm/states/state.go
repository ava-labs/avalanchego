// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package states

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	utxoPrefix      = []byte("utxo")
	statusPrefix    = []byte("status")
	singletonPrefix = []byte("singleton")
	txPrefix        = []byte("tx")

	_ State = &state{}
)

// State persistently maintains a set of UTXOs, transaction, statuses, and
// singletons.
type State interface {
	avax.UTXOState
	avax.StatusState
	avax.SingletonState
	TxState
}

type state struct {
	avax.UTXOState
	avax.StatusState
	avax.SingletonState
	TxState
}

func New(db database.Database, parser txs.Parser, metrics prometheus.Registerer) (State, error) {
	utxoDB := prefixdb.New(utxoPrefix, db)
	statusDB := prefixdb.New(statusPrefix, db)
	singletonDB := prefixdb.New(singletonPrefix, db)
	txDB := prefixdb.New(txPrefix, db)

	utxoState, err := avax.NewMeteredUTXOState(utxoDB, parser.Codec(), metrics)
	if err != nil {
		return nil, err
	}

	statusState, err := avax.NewMeteredStatusState(statusDB, metrics)
	if err != nil {
		return nil, err
	}

	txState, err := NewTxState(txDB, parser, metrics)
	return &state{
		UTXOState:      utxoState,
		StatusState:    statusState,
		SingletonState: avax.NewSingletonState(singletonDB),
		TxState:        txState,
	}, err
}
