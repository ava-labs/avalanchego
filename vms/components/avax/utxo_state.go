// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"errors"
	"iter"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/metric"
)

const (
	utxoCacheSize  = 8192
	indexCacheSize = 64
)

// UTXOState is a thin wrapper around a database to provide, caching,
// serialization, and de-serialization for UTXOs.
type UTXOState interface {
	UTXOReader
	UTXOWriter

	// Checksum returns the current UTXOChecksum.
	Checksum() ids.ID
}

// UTXOReader is a thin wrapper around a database to provide fetching of UTXOs.
type UTXOReader interface {
	UTXOGetter

	// UTXOIDs returns the slice of IDs associated with [addr], starting after
	// [previous].
	// If [previous] is not in the list, starts at beginning.
	// Returns at most [limit] IDs.
	// TODO replace with 1.23 iterator
	UTXOIDs(addr []byte, previous ids.ID, limit int) ([]ids.ID, error)
	// UTXOs returns an iterator starting at startingUTXOID
	UTXOs(startingUTXOID ids.ID) iter.Seq2[*UTXO, error]
}

// UTXOGetter is a thin wrapper around a database to provide fetching of a UTXO.
type UTXOGetter interface {
	// GetUTXO attempts to load a utxo.
	GetUTXO(utxoID ids.ID) (*UTXO, error)
}

type UTXOAdder interface {
	AddUTXO(utxo *UTXO)
}

type UTXODeleter interface {
	DeleteUTXO(utxoID ids.ID)
}

// UTXOWriter is a thin wrapper around a database to provide storage and
// deletion of UTXOs.
type UTXOWriter interface {
	// PutUTXO saves the provided utxo to storage.
	PutUTXO(utxo *UTXO) error

	// DeleteUTXO deletes the provided utxo.
	DeleteUTXO(utxoID ids.ID) error
}

type utxoState struct {
	codec codec.Manager

	// UTXO ID -> *UTXO. If the *UTXO is nil the UTXO doesn't exist
	utxoCache cache.Cacher[ids.ID, *UTXO]
	utxoDB    database.Database

	indexDB    database.Database
	indexCache cache.Cacher[string, linkeddb.LinkedDB]

	trackChecksum bool
	checksum      ids.ID
}

func NewUTXOState(
	utxoDB database.Database,
	indexDB database.Database,
	codec codec.Manager,
	trackChecksum bool,
) (UTXOState, error) {
	s := &utxoState{
		codec:         codec,
		utxoCache:     &cache.LRU[ids.ID, *UTXO]{Size: utxoCacheSize},
		utxoDB:        utxoDB,
		indexDB:       indexDB,
		indexCache:    &cache.LRU[string, linkeddb.LinkedDB]{Size: indexCacheSize},
		trackChecksum: trackChecksum,
	}
	return s, s.initChecksum()
}

func NewMeteredUTXOState(
	namespace string,
	utxoDB database.Database,
	indexDB database.Database,
	codec codec.Manager,
	metrics prometheus.Registerer,
	trackChecksum bool,
) (UTXOState, error) {
	utxoCache, err := metercacher.New[ids.ID, *UTXO](
		metric.AppendNamespace(namespace, "utxo_cache"),
		metrics,
		&cache.LRU[ids.ID, *UTXO]{Size: utxoCacheSize},
	)
	if err != nil {
		return nil, err
	}

	indexCache, err := metercacher.New[string, linkeddb.LinkedDB](
		metric.AppendNamespace(namespace, "index_cache"),
		metrics,
		&cache.LRU[string, linkeddb.LinkedDB]{
			Size: indexCacheSize,
		},
	)
	if err != nil {
		return nil, err
	}

	s := &utxoState{
		codec:         codec,
		utxoCache:     utxoCache,
		utxoDB:        utxoDB,
		indexDB:       indexDB,
		indexCache:    indexCache,
		trackChecksum: trackChecksum,
	}
	return s, s.initChecksum()
}

func (s *utxoState) GetUTXO(utxoID ids.ID) (*UTXO, error) {
	if utxo, found := s.utxoCache.Get(utxoID); found {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
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

func (s *utxoState) PutUTXO(utxo *UTXO) error {
	utxoBytes, err := s.codec.Marshal(codecVersion, utxo)
	if err != nil {
		return err
	}

	utxoID := utxo.InputID()
	s.updateChecksum(utxoID)

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
	if err == database.ErrNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	s.updateChecksum(utxoID)

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
		if utxoID == start {
			continue
		}

		start = ids.Empty
		utxoIDs = append(utxoIDs, utxoID)
	}
	return utxoIDs, iter.Error()
}

func (s *utxoState) UTXOs(startingUTXOID ids.ID) iter.Seq2[*UTXO, error] {
	return func(yield func(*UTXO, error) bool) {
		var itr database.Iterator

		if startingUTXOID != ids.Empty {
			itr = s.utxoDB.NewIteratorWithStart(startingUTXOID[:])
		} else {
			itr = s.utxoDB.NewIterator()
		}

		defer itr.Release()

		for itr.Next() {
			u := &UTXO{}
			_, err := s.codec.Unmarshal(itr.Value(), u)
			if err != nil {
				return
			}

			if !yield(u, errors.Join(err, itr.Error())) {
				return
			}
		}
	}
}

func (s *utxoState) Checksum() ids.ID {
	return s.checksum
}

func (s *utxoState) getIndexDB(addr []byte) linkeddb.LinkedDB {
	addrStr := string(addr)
	if indexList, exists := s.indexCache.Get(addrStr); exists {
		return indexList
	}

	indexDB := prefixdb.NewNested(addr, s.indexDB)
	indexList := linkeddb.NewDefault(indexDB)
	s.indexCache.Put(addrStr, indexList)
	return indexList
}

func (s *utxoState) initChecksum() error {
	if !s.trackChecksum {
		return nil
	}

	it := s.utxoDB.NewIterator()
	defer it.Release()

	for it.Next() {
		utxoID, err := ids.ToID(it.Key())
		if err != nil {
			return err
		}
		s.updateChecksum(utxoID)
	}
	return it.Error()
}

func (s *utxoState) updateChecksum(modifiedID ids.ID) {
	if !s.trackChecksum {
		return
	}

	s.checksum = s.checksum.XOR(modifiedID)
}
