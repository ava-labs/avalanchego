// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/cache/metercacher"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

const (
	statusCacheSize = 8192
)

// StatusState is a thin wrapper around a database to provide, caching,
// serialization, and de-serialization for statuses.
type StatusState interface {
	// Status returns a status from storage.
	GetStatus(id ids.ID) (choices.Status, error)

	// PutStatus saves a status in storage.
	PutStatus(id ids.ID, status choices.Status) error

	// DeleteStatus removes a status from storage.
	DeleteStatus(id ids.ID) error
}

type statusState struct {
	// Holds IDs for which we don't know the status.
	// Invariant: if a key is in [unknownStatusCache],
	// it isn't in [statusCache], and vice versa.
	unknownStatusCache cache.Cacher[ids.ID, struct{}]
	// ID -> Status of thing with that ID
	statusCache cache.Cacher[ids.ID, choices.Status]
	statusDB    database.Database
}

func NewStatusState(db database.Database) StatusState {
	return &statusState{
		unknownStatusCache: &cache.LRU[ids.ID, struct{}]{Size: statusCacheSize},
		statusCache:        &cache.LRU[ids.ID, choices.Status]{Size: statusCacheSize},
		statusDB:           db,
	}
}

func NewMeteredStatusState(db database.Database, metrics prometheus.Registerer) (StatusState, error) {
	statusCache, err := metercacher.New[ids.ID, choices.Status](
		"status_cache",
		metrics,
		&cache.LRU[ids.ID, choices.Status]{Size: statusCacheSize},
	)
	if err != nil {
		return nil, err
	}
	unknownStatusCache, err := metercacher.New[ids.ID, struct{}](
		"unknown_status_cache",
		metrics,
		&cache.LRU[ids.ID, struct{}]{Size: statusCacheSize},
	)
	return &statusState{
		unknownStatusCache: unknownStatusCache,
		statusCache:        statusCache,
		statusDB:           db,
	}, err
}

func (s *statusState) GetStatus(id ids.ID) (choices.Status, error) {
	if status, ok := s.statusCache.Get(id); ok {
		return status, nil
	}
	if _, ok := s.unknownStatusCache.Get(id); ok {
		return choices.Unknown, database.ErrNotFound
	}

	val, err := database.GetUInt32(s.statusDB, id[:])
	if err == database.ErrNotFound {
		s.unknownStatusCache.Put(id, struct{}{})
		return choices.Unknown, database.ErrNotFound
	}
	if err != nil {
		return choices.Unknown, err
	}

	status := choices.Status(val)
	if err := status.Valid(); err != nil {
		return choices.Unknown, err
	}

	s.statusCache.Put(id, status)
	s.unknownStatusCache.Evict(id)
	return status, nil
}

func (s *statusState) PutStatus(id ids.ID, status choices.Status) error {
	s.statusCache.Put(id, status)
	s.unknownStatusCache.Evict(id)
	return database.PutUInt32(s.statusDB, id[:], uint32(status))
}

func (s *statusState) DeleteStatus(id ids.ID) error {
	s.statusCache.Evict(id)
	s.unknownStatusCache.Put(id, struct{}{})
	return s.statusDB.Delete(id[:])
}
