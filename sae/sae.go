// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package sae implements the [Streaming Asynchronous Execution] (SAE) virtual
// machine to be compatible with Avalanche consensus.
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package sae

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/holiman/uint256"
)

func unix(t time.Time) uint64 {
	return uint64(t.Unix()) //nolint:gosec // Guaranteed to be positive
}

// uint256FromBig is a wrapper around [uint256.FromBig] with extra checks, for
// nil input and for overflow.
func uint256FromBig(b *big.Int) (*uint256.Int, error) {
	if b == nil {
		return nil, errors.New("nil big.Int")
	}
	u, overflow := uint256.FromBig(b)
	if overflow {
		return nil, fmt.Errorf("big.Int %v overflows 256 bits", b)
	}
	return u, nil
}

type syncMap[K comparable, V any] struct {
	m  map[K]V
	mu sync.RWMutex

	onStore  func(V)
	onDelete func(V)
}

// newSyncMap creates a concurrent-safe map, which automatically performs
// `onStore` and `onDelete` if [syncMap.Store] and [syncMap.Delete] are called,
// respectively. If either function is nil, or the key to be deleted doesn't
// exist, no operation will be performed.
func newSyncMap[K comparable, V any](onStore func(V), onDelete func(V)) *syncMap[K, V] {
	if onStore == nil {
		onStore = func(V) {}
	}
	if onDelete == nil {
		onDelete = func(V) {}
	}

	return &syncMap[K, V]{
		m:        make(map[K]V),
		onStore:  onStore,
		onDelete: onDelete,
	}
}

func (m *syncMap[K, V]) Load(k K) (V, bool) {
	m.mu.RLock()
	v, ok := m.m[k]
	m.mu.RUnlock()
	return v, ok
}

func (m *syncMap[K, V]) Store(k K, v V) {
	m.onStore(v)
	m.mu.Lock()
	m.m[k] = v
	m.mu.Unlock()
}

func (m *syncMap[K, V]) Delete(k K) {
	m.mu.Lock()
	if v, ok := m.m[k]; ok {
		m.onDelete(v)
	}
	delete(m.m, k)
	m.mu.Unlock()
}
