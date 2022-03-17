// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

const (
	txDeduplicatorSize = 8192
)

var (
	utxoStatePrefix            = []byte("utxo")
	statusStatePrefix          = []byte("status")
	singletonStatePrefix       = []byte("singleton")
	txStatePrefix              = []byte("tx")
	_                    State = &state{}
)

// State persistently maintains a set of UTXOs, transaction, statuses, and
// singletons.
type State interface {
	avax.UTXOState
	avax.StatusState
	avax.SingletonState
	TxState
	DeduplicateTx(tx *UniqueTx) *UniqueTx
}

type state struct {
	avax.UTXOState
	avax.StatusState
	avax.SingletonState
	TxState

	uniqueTxs cache.Deduplicator
}

type StateConfig struct {
	DB                  database.Database
	GenesisCodec, Codec codec.Manager
	Metrics             prometheus.Registerer
}

func NewState(config StateConfig) (State, error) {
	utxoDB := prefixdb.New(utxoStatePrefix, config.DB)
	statusDB := prefixdb.New(statusStatePrefix, config.DB)
	singletonDB := prefixdb.New(singletonStatePrefix, config.DB)
	txDB := prefixdb.New(txStatePrefix, config.DB)

	utxoState, err := avax.NewMeteredUTXOState(utxoDB, config.Codec, config.Metrics)
	if err != nil {
		return nil, err
	}

	statusState, err := avax.NewMeteredStatusState(statusDB, config.Metrics)
	if err != nil {
		return nil, err
	}

	txState, err := NewMeteredTxState(txDB, config.GenesisCodec, config.Metrics)

	return &state{
		UTXOState:      utxoState,
		StatusState:    statusState,
		SingletonState: avax.NewSingletonState(singletonDB),
		TxState:        txState,

		uniqueTxs: &cache.EvictableLRU{
			Size: txDeduplicatorSize,
		},
	}, err
}

// UniqueTx de-duplicates the transaction.
func (s *state) DeduplicateTx(tx *UniqueTx) *UniqueTx {
	return s.uniqueTxs.Deduplicate(tx).(*UniqueTx)
}
