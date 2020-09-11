// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"bytes"
	"errors"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/codec"
)

// Addressable is the interface a feature extension must provide to be able to
// be tracked as a part of the utxo set for a set of addresses
type Addressable interface {
	Addresses() [][]byte
}

const (
	smallerUTXOID uint64 = iota
	smallerStatusID
	smallerFundsID
	largerUTXOID
	largerStatusID
	largerFundsID
)

const (
	stateCacheSize = 10000
	idCacheSize    = 10000
)

type chainState struct {
	*State

	utxoIDPrefix, statusIDPrefix, fundsIDPrefix uint64
	utxoID, statusID, fundsID                   cache.Cacher
}

// UTXO attempts to load a utxo from platform's storage.
func (s *chainState) UTXO(id ids.ID) (*UTXO, error) {
	return s.State.UTXO(UniqueID(id, s.utxoIDPrefix, s.utxoID))
}

// Funds returns the mapping from the 32 byte representation of an
// address to a list of utxo IDs that reference the address.
// All UTXO IDs have IDs greater than [start].
// The returned list contains at most [limit] UTXO IDs.
func (s *chainState) Funds(addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	var addrArr [32]byte
	copy(addrArr[:], addr)
	addrID := ids.NewID(addrArr)
	return s.IDs(UniqueID(addrID, s.fundsIDPrefix, s.fundsID).Bytes(), start.Bytes(), limit)
}

// SpendUTXO consumes the provided platform utxo.
func (s *chainState) SpendUTXO(utxoID ids.ID) error {
	utxo, err := s.UTXO(utxoID)
	if err != nil {
		return s.setStatus(utxoID, choices.Accepted)
	} else if err := s.setUTXO(utxoID, nil); err != nil {
		return err
	}

	if addressable, ok := utxo.Out.(Addressable); ok {
		return s.removeUTXO(addressable.Addresses(), utxoID)
	}
	return nil
}

// FundUTXO adds the provided utxo to the database
func (s *chainState) FundUTXO(utxo *UTXO) error {
	utxoID := utxo.InputID()
	if _, err := s.status(utxoID); err == nil {
		return s.setStatus(utxoID, choices.Unknown)
	} else if err := s.setUTXO(utxoID, utxo); err != nil {
		return err
	}

	if addressable, ok := utxo.Out.(Addressable); ok {
		return s.addUTXO(addressable.Addresses(), utxoID)
	}
	return nil
}

// setUTXO saves the provided utxo to platform's storage.
func (s *chainState) setUTXO(id ids.ID, utxo *UTXO) error {
	return s.SetUTXO(UniqueID(id, s.utxoIDPrefix, s.utxoID), utxo)
}

func (s *chainState) status(id ids.ID) (choices.Status, error) {
	return s.Status(UniqueID(id, s.statusIDPrefix, s.statusID))
}

// setStatus saves the provided platform status to storage.
func (s *chainState) setStatus(id ids.ID, status choices.Status) error {
	return s.State.SetStatus(UniqueID(id, s.statusIDPrefix, s.statusID), status)
}

func (s *chainState) removeUTXO(addrs [][]byte, utxoID ids.ID) error {
	for _, addr := range addrs {
		var addrArr [32]byte
		copy(addrArr[:], addr)
		addrID := ids.NewID(addrArr)
		addrID = UniqueID(addrID, s.fundsIDPrefix, s.fundsID)
		if err := s.RemoveID(addrID.Bytes(), utxoID); err != nil {
			return err
		}
	}
	return nil
}

func (s *chainState) addUTXO(addrs [][]byte, utxoID ids.ID) error {
	for _, addr := range addrs {
		var addrArr [32]byte
		copy(addrArr[:], addr)
		addrID := ids.NewID(addrArr)
		addrID = UniqueID(addrID, s.fundsIDPrefix, s.fundsID)
		if err := s.AddID(addrID.Bytes(), utxoID); err != nil {
			return err
		}
	}
	return nil
}

// PrefixedState wraps a state object. By prefixing the state, there will
// be no collisions between different types of objects that have the same hash.
type PrefixedState struct {
	isSmaller                 bool
	smallerChain, largerChain chainState
}

// NewPrefixedState ...
func NewPrefixedState(db database.Database, codec codec.Codec, myChain, peerChain ids.ID) *PrefixedState {
	state := &State{
		Cache: &cache.LRU{Size: stateCacheSize},
		DB:    db,
		Codec: codec,
	}
	return &PrefixedState{
		isSmaller: bytes.Compare(myChain.Bytes(), peerChain.Bytes()) == -1,
		smallerChain: chainState{
			State: state,

			utxoIDPrefix:   smallerUTXOID,
			statusIDPrefix: smallerStatusID,
			fundsIDPrefix:  smallerFundsID,

			utxoID:   &cache.LRU{Size: idCacheSize},
			statusID: &cache.LRU{Size: idCacheSize},
			fundsID:  &cache.LRU{Size: idCacheSize},
		},
		largerChain: chainState{
			State: state,

			utxoIDPrefix:   largerUTXOID,
			statusIDPrefix: largerStatusID,
			fundsIDPrefix:  largerFundsID,

			utxoID:   &cache.LRU{Size: idCacheSize},
			statusID: &cache.LRU{Size: idCacheSize},
			fundsID:  &cache.LRU{Size: idCacheSize},
		},
	}
}

// UTXO attempts to load a utxo from storage.
func (s *PrefixedState) UTXO(id ids.ID) (*UTXO, error) {
	if s.isSmaller {
		return s.smallerChain.UTXO(id)
	}
	return s.largerChain.UTXO(id)
}

// Funds returns the mapping from the 32 byte representation of an address to a
// list of utxo IDs that reference the address.
// All returned UTXO IDs have IDs greater than [start].
// (ids.Empty is the "least" ID.)
// Returns at most [limit] UTXO IDs.
func (s *PrefixedState) Funds(addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	if s.isSmaller {
		return s.smallerChain.Funds(addr, start, limit)
	}
	return s.largerChain.Funds(addr, start, limit)
}

// SpendUTXO consumes the provided utxo.
func (s *PrefixedState) SpendUTXO(utxoID ids.ID) error {
	if s.isSmaller {
		return s.smallerChain.SpendUTXO(utxoID)
	}
	return s.largerChain.SpendUTXO(utxoID)
}

// FundUTXO adds the provided utxo to the database
func (s *PrefixedState) FundUTXO(utxo *UTXO) error {
	if s.isSmaller {
		return s.largerChain.FundUTXO(utxo)
	}
	return s.smallerChain.FundUTXO(utxo)
}

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
