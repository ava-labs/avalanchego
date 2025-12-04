// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/lru"
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
	Checksum() (ids.ID, error)
	Close() error
}

// UTXOReader is a thin wrapper around a database to provide fetching of UTXOs.
type UTXOReader interface {
	UTXOGetter

	// UTXOIDs returns the slice of IDs associated with [addr], starting after
	// [previous].
	// If [previous] is not in the list, starts at beginning.
	// Returns at most [limit] IDs.
	UTXOIDs(addr []byte, previous ids.ID, limit int) ([]ids.ID, error)
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

// UTXODB is the database where utxos are persisted
type UTXODB interface {
	Get(inputID ids.ID) (*UTXO, error)
	Put(u *UTXO) error
	Delete(key []byte) error
	InitChecksum() error
	UpdateChecksum(utxoID ids.ID)
	Checksum() (ids.ID, error)
	Close() error
}

type utxoState struct {
	// UTXO ID -> *UTXO. If the *UTXO is nil the UTXO doesn't exist
	utxoCache cache.Cacher[ids.ID, *UTXO]
	utxoDB    UTXODB

	indexDB    database.Database
	indexCache cache.Cacher[string, linkeddb.LinkedDB]
}

func NewUTXOState(
	utxoDB database.Database,
	indexDB database.Database,
	codec codec.Manager,
	trackChecksum bool,
) (UTXOState, error) {
	s := &utxoState{
		utxoCache: lru.NewCache[ids.ID, *UTXO](utxoCacheSize),
		utxoDB:    NewUTXODatabase(utxoDB, codec, trackChecksum),

		indexDB:    indexDB,
		indexCache: lru.NewCache[string, linkeddb.LinkedDB](indexCacheSize),
	}
	return s, s.utxoDB.InitChecksum()
}

func NewMeteredUTXOState(
	namespace string,
	utxoDB UTXODB,
	indexDB database.Database,
	metrics prometheus.Registerer,
) (UTXOState, error) {
	utxoCache, err := metercacher.New[ids.ID, *UTXO](
		metric.AppendNamespace(namespace, "utxo_cache"),
		metrics,
		lru.NewCache[ids.ID, *UTXO](utxoCacheSize),
	)
	if err != nil {
		return nil, err
	}

	indexCache, err := metercacher.New[string, linkeddb.LinkedDB](
		metric.AppendNamespace(namespace, "index_cache"),
		metrics,
		lru.NewCache[string, linkeddb.LinkedDB](indexCacheSize),
	)
	if err != nil {
		return nil, err
	}

	s := &utxoState{
		utxoCache: utxoCache,
		utxoDB:    utxoDB,

		indexDB:    indexDB,
		indexCache: indexCache,
	}
	return s, utxoDB.InitChecksum()
}

func (s *utxoState) GetUTXO(utxoID ids.ID) (*UTXO, error) {
	if utxo, found := s.utxoCache.Get(utxoID); found {
		if utxo == nil {
			return nil, database.ErrNotFound
		}
		return utxo, nil
	}

	utxo, err := s.utxoDB.Get(utxoID)
	if err == database.ErrNotFound {
		s.utxoCache.Put(utxoID, nil)
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	s.utxoCache.Put(utxoID, utxo)
	return utxo, nil
}

func (s *utxoState) PutUTXO(utxo *UTXO) error {
	utxoID := utxo.InputID()
	s.utxoDB.UpdateChecksum(utxoID)

	s.utxoCache.Put(utxoID, utxo)
	if err := s.utxoDB.Put(utxo); err != nil {
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

	s.utxoDB.UpdateChecksum(utxoID)

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

func (s *utxoState) Checksum() (ids.ID, error) {
	return s.utxoDB.Checksum()
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

func (s *utxoState) Close() error {
	if err := s.utxoDB.Close(); err != nil {
		return fmt.Errorf("closing utxo db: %w", err)
	}

	if err := s.indexDB.Close(); err != nil {
		return fmt.Errorf("closing index db: %w", err)
	}

	return nil
}

// Deprecated: [FirewoodUTXODB] should be used after the firewood migration.
type UTXODatabase struct {
	db            database.Database
	codec         codec.Manager
	trackChecksum bool
	checksum      ids.ID
}

var _ UTXODB = (*UTXODatabase)(nil)

func NewUTXODatabase(
	db database.Database,
	c codec.Manager,
	trackChecksum bool,
) *UTXODatabase {
	return &UTXODatabase{
		db:            db,
		codec:         c,
		trackChecksum: trackChecksum,
	}
}

func (u *UTXODatabase) Get(inputID ids.ID) (*UTXO, error) {
	bytes, err := u.db.Get(inputID[:])
	if err != nil {
		return nil, err
	}

	utxo := &UTXO{}
	if _, err := u.codec.Unmarshal(bytes, utxo); err != nil {
		return nil, err
	}

	return utxo, nil
}

func (u *UTXODatabase) Put(utxo *UTXO) error {
	utxoBytes, err := u.codec.Marshal(codecVersion, utxo)
	if err != nil {
		return err
	}

	inputID := utxo.InputID()
	return u.db.Put(inputID[:], utxoBytes)
}

func (u *UTXODatabase) Delete(key []byte) error {
	return u.db.Delete(key)
}

func (u *UTXODatabase) InitChecksum() error {
	if !u.trackChecksum {
		return nil
	}

	it := u.db.NewIterator()
	defer it.Release()

	for it.Next() {
		utxoID, err := ids.ToID(it.Key())
		if err != nil {
			return err
		}
		u.UpdateChecksum(utxoID)
	}
	return it.Error()
}

func (u *UTXODatabase) UpdateChecksum(modifiedID ids.ID) {
	if !u.trackChecksum {
		return
	}

	u.checksum = u.checksum.XOR(modifiedID)
}

func (u *UTXODatabase) Checksum() (ids.ID, error) {
	return u.checksum, nil
}

func (u *UTXODatabase) Close() error {
	return u.db.Close()
}
