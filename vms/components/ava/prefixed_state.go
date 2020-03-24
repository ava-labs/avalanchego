// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ava

import (
	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/vms/components/codec"
)

const (
	platformUTXOID uint64 = iota
	platformStatusID
	avmUTXOID
	avmStatusID
)

const (
	stateCacheSize = 10000
	idCacheSize    = 10000
)

// PrefixedState wraps a state object. By prefixing the state, there will
// be no collisions between different types of objects that have the same hash.
type PrefixedState struct {
	State

	platformUTXO, platformStatus, avmUTXO, avmStatus cache.Cacher
}

// NewPrefixedState ...
func NewPrefixedState(db database.Database, codec codec.Codec) *PrefixedState {
	return &PrefixedState{
		State: State{
			Cache: &cache.LRU{Size: stateCacheSize},
			DB:    db,
			Codec: codec,
		},
		platformUTXO:   &cache.LRU{Size: idCacheSize},
		platformStatus: &cache.LRU{Size: idCacheSize},
		avmUTXO:        &cache.LRU{Size: idCacheSize},
		avmStatus:      &cache.LRU{Size: idCacheSize},
	}
}

// PlatformUTXO attempts to load a utxo from platform's storage.
func (s *PrefixedState) PlatformUTXO(id ids.ID) (*UTXO, error) {
	return s.UTXO(UniqueID(id, platformUTXOID, s.platformUTXO))
}

// SetPlatformUTXO saves the provided utxo to platform's storage.
func (s *PrefixedState) SetPlatformUTXO(id ids.ID, utxo *UTXO) error {
	return s.SetUTXO(UniqueID(id, platformUTXOID, s.platformUTXO), utxo)
}

// PlatformStatus returns the platform status from storage.
func (s *PrefixedState) PlatformStatus(id ids.ID) (choices.Status, error) {
	return s.Status(UniqueID(id, platformStatusID, s.platformStatus))
}

// SetPlatformStatus saves the provided platform status to storage.
func (s *PrefixedState) SetPlatformStatus(id ids.ID, status choices.Status) error {
	return s.SetStatus(UniqueID(id, platformStatusID, s.platformStatus), status)
}

// AVMUTXO attempts to load a utxo from AVM's storage.
func (s *PrefixedState) AVMUTXO(id ids.ID) (*UTXO, error) {
	return s.UTXO(UniqueID(id, avmUTXOID, s.platformUTXO))
}

// SetAVMUTXO saves the provided utxo to AVM's storage.
func (s *PrefixedState) SetAVMUTXO(id ids.ID, utxo *UTXO) error {
	return s.SetUTXO(UniqueID(id, avmUTXOID, s.platformUTXO), utxo)
}

// AVMStatus returns the AVM status from storage.
func (s *PrefixedState) AVMStatus(id ids.ID) (choices.Status, error) {
	return s.Status(UniqueID(id, avmStatusID, s.platformStatus))
}

// SetAVMStatus saves the provided platform status to storage.
func (s *PrefixedState) SetAVMStatus(id ids.ID, status choices.Status) error {
	return s.SetStatus(UniqueID(id, avmStatusID, s.platformStatus), status)
}
