// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/chain4travel/caminogo/cache"
	"github.com/chain4travel/caminogo/cache/metercacher"
	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/choices"
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
	// ID -> Status of thing with that ID, or nil if StatusState doesn't have
	// that status.
	statusCache cache.Cacher
	statusDB    database.Database
}

func NewStatusState(db database.Database) StatusState {
	return &statusState{
		statusCache: &cache.LRU{Size: statusCacheSize},
		statusDB:    db,
	}
}

func NewMeteredStatusState(db database.Database, metrics prometheus.Registerer) (StatusState, error) {
	cache, err := metercacher.New(
		"status_cache",
		metrics,
		&cache.LRU{Size: statusCacheSize},
	)
	return &statusState{
		statusCache: cache,
		statusDB:    db,
	}, err
}

func (s *statusState) GetStatus(id ids.ID) (choices.Status, error) {
	if statusIntf, found := s.statusCache.Get(id); found {
		if statusIntf == nil {
			return choices.Unknown, database.ErrNotFound
		}
		return statusIntf.(choices.Status), nil
	}

	val, err := database.GetUInt32(s.statusDB, id[:])
	if err == database.ErrNotFound {
		s.statusCache.Put(id, nil)
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
	return status, nil
}

func (s *statusState) PutStatus(id ids.ID, status choices.Status) error {
	s.statusCache.Put(id, status)
	return database.PutUInt32(s.statusDB, id[:], uint32(status))
}

func (s *statusState) DeleteStatus(id ids.ID) error {
	s.statusCache.Put(id, nil)
	return s.statusDB.Delete(id[:])
}
