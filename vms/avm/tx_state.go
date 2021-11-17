// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const (
	txCacheSize = 8192
)

var _ TxState = &txState{}

// TxState is a thin wrapper around a database to provide, caching,
// serialization, and de-serialization of transactions.
type TxState interface {
	// Tx attempts to load a transaction from storage.
	GetTx(txID ids.ID) (*Tx, error)

	// PutTx saves the provided transaction to storage.
	PutTx(txID ids.ID, tx *Tx) error

	// DeleteTx removes the provided transaction from storage.
	DeleteTx(txID ids.ID) error
}

type txState struct {
	codec codec.Manager

	// Caches TxID -> *Tx. If the *Tx is nil, that means the tx is not in
	// storage.
	txCache cache.Cacher
	txDB    database.Database
}

func NewTxState(db database.Database, codec codec.Manager) TxState {
	return &txState{
		codec: codec,

		txCache: &cache.LRU{
			Size: txCacheSize,
		},
		txDB: db,
	}
}

func NewMeteredTxState(db database.Database, codec codec.Manager, metrics prometheus.Registerer) (TxState, error) {
	cache, err := metercacher.New(
		"tx_cache",
		metrics,
		&cache.LRU{Size: txCacheSize},
	)
	return &txState{
		codec: codec,

		txCache: cache,
		txDB:    db,
	}, err
}

func (s *txState) GetTx(txID ids.ID) (*Tx, error) {
	if txIntf, found := s.txCache.Get(txID); found {
		if txIntf == nil {
			return nil, database.ErrNotFound
		}
		return txIntf.(*Tx), nil
	}

	txBytes, err := s.txDB.Get(txID[:])
	if err == database.ErrNotFound {
		s.txCache.Put(txID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// The key was in the database
	tx := &Tx{}
	cv, err := s.codec.Unmarshal(txBytes, tx)
	if err != nil {
		return nil, err
	}
	unsignedBytes, err := s.codec.Marshal(cv, &tx.UnsignedTx)
	if err != nil {
		return nil, err
	}
	tx.Initialize(unsignedBytes, txBytes)

	s.txCache.Put(txID, tx)
	return tx, nil
}

func (s *txState) PutTx(txID ids.ID, tx *Tx) error {
	s.txCache.Put(txID, tx)
	return s.txDB.Put(txID[:], tx.Bytes())
}

func (s *txState) DeleteTx(txID ids.ID) error {
	s.txCache.Put(txID, nil)
	return s.txDB.Delete(txID[:])
}
