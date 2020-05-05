// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"errors"

	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/wrappers"
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
	c := Codec{}
	tx, err := c.UnmarshalTx(bytes)
	if err != nil {
		return nil, err
	}

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
	return s.vm.db.Put(id.Bytes(), tx.bytes)
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
	c := Codec{}
	utxo, err := c.UnmarshalUTXO(bytes)
	if err != nil {
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
	s.c.Put(id, utxo)
	return s.vm.db.Put(id.Bytes(), utxo.Bytes())
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

	// The key was in the database
	p := wrappers.Packer{Bytes: bytes}
	status := choices.Status(p.UnpackInt())

	if p.Offset != len(bytes) {
		p.Add(errExtraSpace)
	}
	if p.Errored() {
		return choices.Unknown, p.Err
	}

	s.c.Put(id, status)
	return status, nil
}

// SetStatus saves a status in storage.
func (s *state) SetStatus(id ids.ID, status choices.Status) error {
	if status == choices.Unknown {
		s.c.Evict(id)
		return s.vm.db.Delete(id.Bytes())
	}

	p := wrappers.Packer{Bytes: make([]byte, 4)}

	p.PackInt(uint32(status))

	if p.Offset != len(p.Bytes) {
		p.Add(errExtraSpace)
	}

	if p.Errored() {
		return p.Err
	}

	s.c.Put(id, status)
	return s.vm.db.Put(id.Bytes(), p.Bytes)
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

	p := wrappers.Packer{Bytes: bytes}

	idSlice := []ids.ID{}
	for i := p.UnpackInt(); i > 0 && !p.Errored(); i-- {
		id, _ := ids.ToID(p.UnpackFixedBytes(hashing.HashLen))
		idSlice = append(idSlice, id)
	}

	if p.Offset != len(bytes) {
		p.Add(errExtraSpace)
	}
	if p.Errored() {
		return nil, p.Err
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

	size := wrappers.IntLen + hashing.HashLen*len(idSlice)
	p := wrappers.Packer{Bytes: make([]byte, size)}

	p.PackInt(uint32(len(idSlice)))
	for _, id := range idSlice {
		p.PackFixedBytes(id.Bytes())
	}

	if p.Offset != len(p.Bytes) {
		p.Add(errExtraSpace)
	}

	if p.Errored() {
		return p.Err
	}

	s.c.Put(id, idSlice)
	return s.vm.db.Put(id.Bytes(), p.Bytes)
}
