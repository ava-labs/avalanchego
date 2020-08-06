// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ava

import (
	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/codec"
)

// Addressable is the interface a feature extension must provide to be able to
// be tracked as a part of the utxo set for a set of addresses
type Addressable interface {
	Addresses() [][]byte
}

const (
	platformUTXOID uint64 = iota
	platformStatusID
	platformFundsID
	avmUTXOID
	avmStatusID
	avmFundsID
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
	platform, avm chainState
}

// NewPrefixedState ...
func NewPrefixedState(db database.Database, codec codec.Codec) *PrefixedState {
	state := &State{
		Cache: &cache.LRU{Size: stateCacheSize},
		DB:    db,
		Codec: codec,
	}
	return &PrefixedState{
		platform: chainState{
			State: state,

			utxoIDPrefix:   platformUTXOID,
			statusIDPrefix: platformStatusID,
			fundsIDPrefix:  platformFundsID,

			utxoID:   &cache.LRU{Size: idCacheSize},
			statusID: &cache.LRU{Size: idCacheSize},
			fundsID:  &cache.LRU{Size: idCacheSize},
		},
		avm: chainState{
			State: state,

			utxoIDPrefix:   avmUTXOID,
			statusIDPrefix: avmStatusID,
			fundsIDPrefix:  avmFundsID,

			utxoID:   &cache.LRU{Size: idCacheSize},
			statusID: &cache.LRU{Size: idCacheSize},
			fundsID:  &cache.LRU{Size: idCacheSize},
		},
	}
}

// PlatformUTXO attempts to load a utxo from platform's storage.
func (s *PrefixedState) PlatformUTXO(id ids.ID) (*UTXO, error) {
	return s.platform.UTXO(id)
}

// PlatformFunds returns the mapping from the 32 byte representation of an
// address to a list of utxo IDs that reference the address.
// All returned UTXO IDs have IDs greater than [start].
// (ids.Empty is the "least" ID.)
// Returns at most [limit] UTXO IDs.
func (s *PrefixedState) PlatformFunds(addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	return s.platform.Funds(addr, start, limit)
}

// SpendPlatformUTXO consumes the provided platform utxo.
func (s *PrefixedState) SpendPlatformUTXO(utxoID ids.ID) error {
	return s.platform.SpendUTXO(utxoID)
}

// FundPlatformUTXO adds the provided utxo to the database
func (s *PrefixedState) FundPlatformUTXO(utxo *UTXO) error {
	return s.platform.FundUTXO(utxo)
}

// AVMUTXO attempts to load a utxo from avm's storage.
func (s *PrefixedState) AVMUTXO(id ids.ID) (*UTXO, error) {
	return s.avm.UTXO(id)
}

// AVMFunds returns the mapping from the 32 byte representation of an
// address to a list of utxo IDs that reference the address.
// All returned UTXO IDs have IDs greater than [start].
// (ids.Empty is the "least" ID.)
// Returns at most [limit] UTXO IDs.
func (s *PrefixedState) AVMFunds(addr []byte, start ids.ID, limit int) ([]ids.ID, error) {
	return s.avm.Funds(addr, start, limit)
}

// SpendAVMUTXO consumes the provided platform utxo.
func (s *PrefixedState) SpendAVMUTXO(utxoID ids.ID) error {
	return s.avm.SpendUTXO(utxoID)
}

// FundAVMUTXO adds the provided utxo to the database
func (s *PrefixedState) FundAVMUTXO(utxo *UTXO) error {
	return s.avm.FundUTXO(utxo)
}
