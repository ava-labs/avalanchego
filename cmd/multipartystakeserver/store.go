// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"fmt"
	"sync"
)

// PartyStore is a thread-safe in-memory store for Party instances.
type PartyStore struct {
	mu      sync.RWMutex
	parties map[string]*Party
}

// NewPartyStore creates a new empty PartyStore.
func NewPartyStore() *PartyStore {
	return &PartyStore{
		parties: make(map[string]*Party),
	}
}

// Put stores a party. It overwrites any existing party with the same ID.
func (s *PartyStore) Put(p *Party) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parties[p.ID] = p
}

// Get returns a party by ID, or an error if not found.
func (s *PartyStore) Get(id string) (*Party, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.parties[id]
	if !ok {
		return nil, fmt.Errorf("party not found")
	}
	return p, nil
}

// WithLock calls fn while holding the write lock. This allows callers to
// perform read-modify-write operations atomically on a party.
func (s *PartyStore) WithLock(id string, fn func(p *Party) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.parties[id]
	if !ok {
		return fmt.Errorf("party not found")
	}
	return fn(p)
}
