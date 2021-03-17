// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"bytes"
	"errors"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	codecVersion = 0
)

// Addressable is the interface a feature extension must provide to be able to
// be tracked as a part of the utxo set for a set of addresses
type Addressable interface {
	Addresses() [][]byte
}

var (
	errCacheTypeMismatch = errors.New("type returned from cache doesn't match the expected type")
)

func NewState(db database.Database, genesisCodec, codec codec.Manager, utxoCacheSize, statusCacheSize, idCacheSize int) *State {
	return &State{
		UTXOCache:    &cache.LRU{Size: utxoCacheSize},
		UTXODB:       prefixdb.New([]byte("utxo"), db),
		StatusCache:  &cache.LRU{Size: statusCacheSize},
		StatusDB:     prefixdb.New([]byte("status"), db),
		IDCache:      &cache.LRU{Size: idCacheSize},
		IDDB:         prefixdb.New([]byte("id"), db),
		GenesisCodec: genesisCodec,
		Codec:        codec,
	}
}

// State is a thin wrapper around a database to provide, caching, serialization,
// and de-serialization.
type State struct {
	UTXOCache    cache.Cacher
	UTXODB       database.Database
	StatusCache  cache.Cacher
	StatusDB     database.Database
	IDCache      cache.Cacher
	IDDB         database.Database
	GenesisCodec codec.Manager
	Codec        codec.Manager
}

// UTXO attempts to load a utxo from storage.
func (s *State) UTXO(id ids.ID) (*UTXO, error) {
	if utxoIntf, found := s.UTXOCache.Get(id); found {
		if utxo, ok := utxoIntf.(*UTXO); ok {
			return utxo, nil
		}
		return nil, errCacheTypeMismatch
	}

	bytes, err := s.UTXODB.Get(id[:])
	if err != nil {
		return nil, err
	}

	// The key was in the database
	utxo := &UTXO{}
	if _, err := s.Codec.Unmarshal(bytes, utxo); err != nil {
		return nil, err
	}

	s.UTXOCache.Put(id, utxo)
	return utxo, nil
}

// SetUTXO saves the provided utxo to storage.
func (s *State) SetUTXO(id ids.ID, utxo *UTXO) error {
	if utxo == nil {
		s.UTXOCache.Evict(id)
		return s.UTXODB.Delete(id[:])
	}

	bytes, err := s.Codec.Marshal(codecVersion, utxo)
	if err != nil {
		return err
	}

	s.UTXOCache.Put(id, utxo)
	return s.UTXODB.Put(id[:], bytes)
}

// Status returns a status from storage.
func (s *State) Status(id ids.ID) (choices.Status, error) {
	if statusIntf, found := s.StatusCache.Get(id); found {
		if status, ok := statusIntf.(choices.Status); ok {
			return status, nil
		}
		return choices.Unknown, errCacheTypeMismatch
	}

	bytes, err := s.StatusDB.Get(id[:])
	if err != nil {
		return choices.Unknown, err
	}

	var status choices.Status
	if _, err := s.Codec.Unmarshal(bytes, &status); err != nil {
		return choices.Unknown, err
	}

	s.StatusCache.Put(id, status)
	return status, nil
}

// SetStatus saves a status in storage.
func (s *State) SetStatus(id ids.ID, status choices.Status) error {
	if status == choices.Unknown {
		s.StatusCache.Evict(id)
		return s.StatusDB.Delete(id[:])
	}

	bytes, err := s.Codec.Marshal(codecVersion, status)
	if err != nil {
		return err
	}

	s.StatusCache.Put(id, status)
	return s.StatusDB.Put(id[:], bytes)
}

// IDs returns the slice of IDs associated with [key], starting after [start].
// If start is ids.Empty, starts at beginning.
// Returns at most [limit] IDs.
func (s *State) IDs(key []byte, start []byte, limit int) ([]ids.ID, error) {
	idSlice := []ids.ID(nil)
	idSliceDB := prefixdb.NewNested(key, s.IDDB)
	iter := idSliceDB.NewIteratorWithStart(start)
	defer iter.Release()

	numFetched := 0
	for numFetched < limit && iter.Next() {
		if keyID, err := ids.ToID(iter.Key()); err != nil {
			// Ignore any database closing error to return the original error
			_ = idSliceDB.Close()
			return nil, err
		} else if !bytes.Equal(keyID[:], start) { // don't return [start]
			idSlice = append(idSlice, keyID)
			numFetched++
		}
	}

	errs := wrappers.Errs{}
	errs.Add(
		iter.Error(),
		idSliceDB.Close(),
	)
	return idSlice, errs.Err
}

// AddID saves an ID to the prefixed database
func (s *State) AddID(key []byte, id ids.ID) error {
	idSliceDB := prefixdb.NewNested(key, s.IDDB)
	errs := wrappers.Errs{}
	errs.Add(
		idSliceDB.Put(id[:], nil),
		idSliceDB.Close(),
	)
	return errs.Err
}

// RemoveID removes an ID from the prefixed database
func (s *State) RemoveID(key []byte, id ids.ID) error {
	idSliceDB := prefixdb.NewNested(key, s.IDDB)
	errs := wrappers.Errs{}
	errs.Add(
		idSliceDB.Delete(id[:]),
		idSliceDB.Close(),
	)
	return errs.Err
}
