// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	utxoPrefix  = []byte("utxo")
	indexPrefix = []byte("index")
)

const (
	utxoCacheSize  = 1024
	indexCacheSize = 64
)

// UTXOState is a thin wrapper around a database to provide, caching,
// serialization, and de-serialization for UTXOs.
type UTXOState interface {
	// GetUTXO attempts to load a utxo from storage.
	GetUTXO(utxoID ids.ID) (*UTXO, error)

	// PutUTXO saves the provided utxo to storage.
	PutUTXO(utxoID ids.ID, utxo *UTXO) error

	// DeleteUTXO deletes the provided utxo from storage.
	DeleteUTXO(utxoID ids.ID) error

	// IDs returns the slice of IDs associated with [key], starting at [start].
	// If start is not in the list, starts at beginning.
	// Returns at most [limit] IDs.
	UTXOIDs(addr []byte, start ids.ID, limit int) ([]ids.ID, error)
}

type utxoState struct {
	codec codec.Manager

	utxoCache cache.Cacher
	utxoDB    database.Database

	indexDB    database.Database
	indexCache cache.Cacher
}

func NewUTXOState(db database.Database, codec codec.Manager) UTXOState {
	return &utxoState{
		codec: codec,

		utxoCache: &cache.LRU{Size: utxoCacheSize},
		utxoDB:    prefixdb.New(utxoPrefix, db),

		indexDB:    prefixdb.New(indexPrefix, db),
		indexCache: &cache.LRU{Size: indexCacheSize},
	}
}

func (s *utxoState) GetUTXO(utxoID ids.ID) (*UTXO, error) {
	if utxoIntf, found := s.utxoCache.Get(utxoID); found {
		if utxoIntf == nil {
			return nil, database.ErrNotFound
		}
		return utxoIntf.(*UTXO), nil
	}

	bytes, err := s.utxoDB.Get(utxoID[:])
	if err == database.ErrNotFound {
		s.utxoCache.Put(utxoID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	// The key was in the database
	utxo := &UTXO{}
	if _, err := s.codec.Unmarshal(bytes, utxo); err != nil {
		return nil, err
	}

	s.utxoCache.Put(utxoID, utxo)
	return utxo, nil
}

func (s *utxoState) PutUTXO(utxoID ids.ID, utxo *UTXO) error {
	utxoBytes, err := s.codec.Marshal(codecVersion, utxo)
	if err != nil {
		return err
	}

	s.utxoCache.Put(utxoID, utxo)
	if err := s.utxoDB.Put(utxoID[:], utxoBytes); err != nil {
		return err
	}

	addressable, ok := utxo.Out.(Addressable)
	if !ok {
		return nil
	}

	addresses := addressable.Addresses()
	for _, addr := range addresses {
		indexList := s.getIndexDB(addr)
		if err := indexList.Put(utxoID[:], nil); err != nil {
			return err
		}
	}
	return nil
}

func (s *utxoState) DeleteUTXO(utxoID ids.ID) error {
	utxo, err := s.GetUTXO(utxoID)
	if err != nil {
		return err
	}

	s.utxoCache.Put(utxoID, nil)
	if err := s.utxoDB.Delete(utxoID[:]); err != nil {
		return err
	}

	addressable, ok := utxo.Out.(Addressable)
	if !ok {
		return nil
	}

	addresses := addressable.Addresses()
	for _, addr := range addresses {
		indexList := s.getIndexDB(addr)
		if err := indexList.Delete(utxoID[:]); err != nil {
			return err
		}
	}
	return nil
}

func (s *utxoState) UTXOIDs(addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	indexList := s.getIndexDB(addr)
	iter := indexList.NewIteratorWithStart(start[:])
	defer iter.Release()

	utxoIDs := []ids.ID(nil)
	for len(utxoIDs) < limit && iter.Next() {
		utxoID, err := ids.ToID(iter.Key())
		if err != nil {
			return nil, err
		}
		utxoIDs = append(utxoIDs, utxoID)
	}
	return utxoIDs, iter.Error()
}

func (s *utxoState) getIndexDB(addr []byte) linkeddb.LinkedDB {
	addrStr := string(addr)
	if indexList, exists := s.indexCache.Get(addrStr); exists {
		return indexList.(linkeddb.LinkedDB)
	}

	indexDB := prefixdb.NewNested(addr, s.indexDB)
	indexList := linkeddb.NewDefault(indexDB)
	s.indexCache.Put(addrStr, indexList)
	return indexList
}
