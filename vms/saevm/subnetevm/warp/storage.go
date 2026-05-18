// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/precompileconfig"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const (
	dbPrefix  = "warp"
	cacheSize = 500
)

var _ precompileconfig.WarpMessageWriter = (*Storage)(nil)

// Storage persists and fetches warp messages.
type Storage struct {
	db        database.Database
	cache     *lru.Cache[ids.ID, *warp.UnsignedMessage]
	overrides map[ids.ID]*warp.UnsignedMessage
}

// NewStorage creates a new Storage backed by the provided database.
func NewStorage(db database.Database, msgs ...*warp.UnsignedMessage) *Storage {
	overrides := make(map[ids.ID]*warp.UnsignedMessage, len(msgs))
	for _, m := range msgs {
		overrides[m.ID()] = m
	}
	return &Storage{
		db:        prefixdb.New([]byte(dbPrefix), db),
		cache:     lru.NewCache[ids.ID, *warp.UnsignedMessage](cacheSize),
		overrides: overrides,
	}
}

func (b *Storage) AddMessage(m *warp.UnsignedMessage) error {
	id := m.ID()
	if err := b.db.Put(id[:], m.Bytes()); err != nil {
		return fmt.Errorf("writing message: %w", err)
	}
	b.cache.Put(id, m)
	return nil
}

func (b *Storage) GetMessage(id ids.ID) (*warp.UnsignedMessage, error) {
	if m, ok := b.cache.Get(id); ok {
		return m, nil
	}
	if m, ok := b.overrides[id]; ok {
		return m, nil
	}

	bytes, err := b.db.Get(id[:])
	if err != nil {
		return nil, err
	}

	m, err := warp.ParseUnsignedMessage(bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing message %s: %w", id, err)
	}
	b.cache.Put(id, m)
	return m, nil
}
