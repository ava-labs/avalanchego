// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ava

import (
	"bytes"
	"errors"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/codec"
)

var (
	errCacheTypeMismatch = errors.New("type returned from cache doesn't match the expected type")
	errZeroID            = errors.New("database key ID value not initialized")
)

// UniqueID returns a unique identifier
func UniqueID(id ids.ID, prefix uint64, cacher cache.Cacher) ids.ID {
	if cachedIDIntf, found := cacher.Get(id); found {
		return cachedIDIntf.(ids.ID)
	}
	uID := id.Prefix(prefix)
	cacher.Put(id, uID)
	return uID
}

// State is a thin wrapper around a database to provide, caching, serialization,
// and de-serialization.
type State struct {
	Cache cache.Cacher
	DB    database.Database
	Codec codec.Codec
}

// UTXO attempts to load a utxo from storage.
func (s *State) UTXO(id ids.ID) (*UTXO, error) {
	if utxoIntf, found := s.Cache.Get(id); found {
		if utxo, ok := utxoIntf.(*UTXO); ok {
			return utxo, nil
		}
		return nil, errCacheTypeMismatch
	}

	bytes, err := s.DB.Get(id.Bytes())
	if err != nil {
		return nil, err
	}

	// The key was in the database
	utxo := &UTXO{}
	if err := s.Codec.Unmarshal(bytes, utxo); err != nil {
		return nil, err
	}

	s.Cache.Put(id, utxo)
	return utxo, nil
}

// SetUTXO saves the provided utxo to storage.
func (s *State) SetUTXO(id ids.ID, utxo *UTXO) error {
	if utxo == nil {
		s.Cache.Evict(id)
		return s.DB.Delete(id.Bytes())
	}

	bytes, err := s.Codec.Marshal(utxo)
	if err != nil {
		return err
	}

	s.Cache.Put(id, utxo)
	return s.DB.Put(id.Bytes(), bytes)
}

// Status returns a status from storage.
func (s *State) Status(id ids.ID) (choices.Status, error) {
	if statusIntf, found := s.Cache.Get(id); found {
		if status, ok := statusIntf.(choices.Status); ok {
			return status, nil
		}
		return choices.Unknown, errCacheTypeMismatch
	}

	bytes, err := s.DB.Get(id.Bytes())
	if err != nil {
		return choices.Unknown, err
	}

	var status choices.Status
	if err := s.Codec.Unmarshal(bytes, &status); err != nil {
		return choices.Unknown, err
	}

	s.Cache.Put(id, status)
	return status, nil
}

// SetStatus saves a status in storage.
func (s *State) SetStatus(id ids.ID, status choices.Status) error {
	if status == choices.Unknown {
		s.Cache.Evict(id)
		return s.DB.Delete(id.Bytes())
	}

	bytes, err := s.Codec.Marshal(status)
	if err != nil {
		return err
	}

	s.Cache.Put(id, status)
	return s.DB.Put(id.Bytes(), bytes)
}

// IDs returns the slice of IDs associated with [id], starting after [start].
// If start is ids.Empty, starts at beginning.
// Returns at most [limit] IDs.
func (s *State) IDs(key []byte, start []byte, limit int) ([]ids.ID, error) {
	idSlice := []ids.ID(nil)
	iter := prefixdb.NewNested(key, s.DB).NewIteratorWithStart(start)
	defer iter.Release()

	numFetched := 0
	for numFetched < limit && iter.Next() {
		if keyID, err := ids.ToID(iter.Key()); err != nil {
			return nil, err
		} else if !bytes.Equal(keyID.Bytes(), start) { // don't return [start]
			idSlice = append(idSlice, keyID)
			numFetched++
		}
	}
	return idSlice, nil
}

// AddID saves an ID to the prefixed database
func (s *State) AddID(key []byte, ID ids.ID) error {
	if ID.IsZero() {
		return errZeroID
	}
	db := prefixdb.NewNested(key, s.DB)
	return db.Put(ID.Bytes(), nil)
}

// RemoveID removes an ID from the prefixed database
func (s *State) RemoveID(key []byte, ID ids.ID) error {
	if ID.IsZero() {
		return errZeroID
	}
	db := prefixdb.NewNested(key, s.DB)
	return db.Delete(ID.Bytes())
}
