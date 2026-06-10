// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"fmt"

	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

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
	const (
		dbPrefix  = "warp"
		cacheSize = 500
	)
	return &Storage{
		db:        prefixdb.New([]byte(dbPrefix), db),
		cache:     lru.NewCache[ids.ID, *warp.UnsignedMessage](cacheSize),
		overrides: overrides,
	}
}

// Add adds the given messages to storage.
func (b *Storage) Add(ms ...*warp.UnsignedMessage) error {
	batch := b.db.NewBatch()
	for i, m := range ms {
		id := m.ID()
		if err := batch.Put(id[:], m.Bytes()); err != nil {
			return fmt.Errorf("writing message %s (%d) to batch: %w", id, i, err)
		}
	}
	if err := batch.Write(); err != nil {
		return fmt.Errorf("committing batch: %w", err)
	}

	// Cache after the DB write has succeeded to ensure the cache is consistent.
	for _, m := range ms {
		b.cache.Put(m.ID(), m)
	}
	return nil
}

// Get returns the message with the given ID.
func (b *Storage) Get(id ids.ID) (*warp.UnsignedMessage, error) {
	if m, ok := b.cache.Get(id); ok {
		return m, nil
	}
	if m, ok := b.overrides[id]; ok {
		return m, nil
	}

	bytes, err := b.db.Get(id[:])
	if err != nil {
		return nil, fmt.Errorf("loading message: %w", err)
	}

	m, err := warp.ParseUnsignedMessage(bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing message: %w", err)
	}
	b.cache.Put(id, m)
	return m, nil
}
