// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ava

import (
	"errors"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/vms/components/codec"
)

var (
	errCacheTypeMismatch = errors.New("type returned from cache doesn't match the expected type")
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
	s.Codec.Unmarshal(bytes, &status)

	s.Cache.Put(id, status)
	return status, nil
}

// SetStatus saves a status in storage.
func (s *State) SetStatus(id ids.ID, status choices.Status) error {
	if status == choices.Unknown {
		s.Cache.Evict(id)
		return s.DB.Delete(id.Bytes())
	}

	s.Cache.Put(id, status)

	bytes, err := s.Codec.Marshal(status)
	if err != nil {
		return err
	}
	return s.DB.Put(id.Bytes(), bytes)
}
