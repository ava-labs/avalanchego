// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
)

var (
	errCacheTypeMismatch = errors.New("type returned from cache doesn't match the expected type")
)

// state is a thin wrapper around a database to provide, caching, serialization,
// and de-serialization.
type state struct {
	c  cache.Cacher
	vm *VM
}

// Tx attempts to load a transaction from storage.
func (s *state) Tx(id ids.ID) (*Tx, error) {
	if txIntf, found := s.c.Get(id); found {
		if tx, ok := txIntf.(*Tx); ok {
			return tx, nil
		}
		return nil, errCacheTypeMismatch
	}

	bytes, err := s.vm.db.Get(id.Bytes())
	if err != nil {
		return nil, err
	}

	// The key was in the database
	tx := &Tx{}
	if err := s.vm.codec.Unmarshal(bytes, tx); err != nil {
		return nil, err
	}
	tx.Initialize(bytes)

	s.c.Put(id, tx)
	return tx, nil
}

// SetTx saves the provided transaction to storage.
func (s *state) SetTx(id ids.ID, tx *Tx) error {
	if tx == nil {
		s.c.Evict(id)
		return s.vm.db.Delete(id.Bytes())
	}

	s.c.Put(id, tx)
	return s.vm.db.Put(id.Bytes(), tx.Bytes())
}

// UTXO attempts to load a utxo from storage.
func (s *state) UTXO(id ids.ID) (*UTXO, error) {
	if utxoIntf, found := s.c.Get(id); found {
		if utxo, ok := utxoIntf.(*UTXO); ok {
			return utxo, nil
		}
		return nil, errCacheTypeMismatch
	}

	bytes, err := s.vm.db.Get(id.Bytes())
	if err != nil {
		return nil, err
	}

	// The key was in the database
	utxo := &UTXO{}
	if err := s.vm.codec.Unmarshal(bytes, utxo); err != nil {
		return nil, err
	}

	s.c.Put(id, utxo)
	return utxo, nil
}

// SetUTXO saves the provided utxo to storage.
func (s *state) SetUTXO(id ids.ID, utxo *UTXO) error {
	if utxo == nil {
		s.c.Evict(id)
		return s.vm.db.Delete(id.Bytes())
	}

	bytes, err := s.vm.codec.Marshal(utxo)
	if err != nil {
		return err
	}

	s.c.Put(id, utxo)
	return s.vm.db.Put(id.Bytes(), bytes)
}

// Status returns a status from storage.
func (s *state) Status(id ids.ID) (choices.Status, error) {
	if statusIntf, found := s.c.Get(id); found {
		if status, ok := statusIntf.(choices.Status); ok {
			return status, nil
		}
		return choices.Unknown, errCacheTypeMismatch
	}

	bytes, err := s.vm.db.Get(id.Bytes())
	if err != nil {
		return choices.Unknown, err
	}

	var status choices.Status
	s.vm.codec.Unmarshal(bytes, &status)

	s.c.Put(id, status)
	return status, nil
}

// SetStatus saves a status in storage.
func (s *state) SetStatus(id ids.ID, status choices.Status) error {
	if status == choices.Unknown {
		s.c.Evict(id)
		return s.vm.db.Delete(id.Bytes())
	}

	s.c.Put(id, status)

	bytes, err := s.vm.codec.Marshal(status)
	if err != nil {
		return err
	}
	return s.vm.db.Put(id.Bytes(), bytes)
}

// IDs returns a slice of IDs from storage
func (s *state) IDs(id ids.ID) ([]ids.ID, error) {
	if idsIntf, found := s.c.Get(id); found {
		if idSlice, ok := idsIntf.([]ids.ID); ok {
			return idSlice, nil
		}
		return nil, errCacheTypeMismatch
	}

	bytes, err := s.vm.db.Get(id.Bytes())
	if err != nil {
		return nil, err
	}

	idSlice := []ids.ID(nil)
	if err := s.vm.codec.Unmarshal(bytes, &idSlice); err != nil {
		return nil, err
	}

	s.c.Put(id, idSlice)
	return idSlice, nil
}

// SetIDs saves a slice of IDs to the database.
func (s *state) SetIDs(id ids.ID, idSlice []ids.ID) error {
	if len(idSlice) == 0 {
		s.c.Evict(id)
		return s.vm.db.Delete(id.Bytes())
	}

	s.c.Put(id, idSlice)

	bytes, err := s.vm.codec.Marshal(idSlice)
	if err != nil {
		return err
	}

	return s.vm.db.Put(id.Bytes(), bytes)
}
