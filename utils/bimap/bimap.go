// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bimap

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils"
)

var _ BiMap[int, int] = (*biMap[int, int])(nil)

type Entry[K, V any] struct {
	Key   K
	Value V
}

// BiMap is a bi-directional map.
type BiMap[K, V any] interface {
	// Put the key value pair into the map. If either [key] or [val] was
	// previously in the map, the previous entries will be removed and returned.
	//
	// Note: Unlike normal maps, it's possible that Put removes 0, 1, or 2
	// existing entries to ensure that mappings are one-to-one.
	Put(key K, val V) (removed []Entry[K, V])
	// GetKey that maps to the provided value.
	GetKey(val V) (key K, exists bool)
	// GetValue that is mapped to the provided key.
	GetValue(key K) (val V, exists bool)
	// HasKey returns true if [key] is in the map.
	HasKey(key K) (exists bool)
	// HasValue returns true if [val] is in the map.
	HasValue(val V) (exists bool)
	// DeleteKey removes [key] from the map and returns the value it mapped to.
	DeleteKey(key K) (val V, deleted bool)
	// DeleteValue removes [val] from the map and returns the key that mapped to
	// it.
	DeleteValue(val V) (key K, deleted bool)
	// Inverse the keys and values of this map.
	//
	// Note: The returned map will modify the same underlying state.
	Inverse() BiMap[V, K]
	// Len return the number of entries in this map.
	Len() int
}

type biMap[K, V comparable] struct {
	lock       *sync.RWMutex
	keyToValue map[K]V
	valueToKey map[V]K
}

func New[K, V comparable]() BiMap[K, V] {
	return &biMap[K, V]{
		lock:       new(sync.RWMutex),
		keyToValue: make(map[K]V),
		valueToKey: make(map[V]K),
	}
}

func (m *biMap[K, V]) Put(key K, val V) (removed []Entry[K, V]) {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.put(key, val)
}

func (m *biMap[K, V]) GetKey(val V) (K, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	key, ok := m.valueToKey[val]
	return key, ok
}

func (m *biMap[K, V]) GetValue(key K) (V, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	val, ok := m.keyToValue[key]
	return val, ok
}

func (m *biMap[K, _]) HasKey(key K) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()

	_, ok := m.keyToValue[key]
	return ok
}

func (m *biMap[_, V]) HasValue(val V) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()

	_, ok := m.valueToKey[val]
	return ok
}

func (m *biMap[K, V]) DeleteKey(key K) (V, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.deleteKey(key)
}

func (m *biMap[K, V]) DeleteValue(val V) (K, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.deleteValue(val)
}

func (m *biMap[K, V]) Inverse() BiMap[V, K] {
	return &biMap[V, K]{
		lock:       m.lock,
		keyToValue: m.valueToKey,
		valueToKey: m.keyToValue,
	}
}

func (m *biMap[K, V]) Len() int {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return len(m.keyToValue)
}

func (m *biMap[K, V]) put(key K, val V) []Entry[K, V] {
	var removed []Entry[K, V]
	oldVal, oldValDeleted := m.deleteKey(key)
	if oldValDeleted {
		removed = append(removed, Entry[K, V]{
			Key:   key,
			Value: oldVal,
		})
	}
	oldKey, oldKeyDeleted := m.deleteValue(val)
	if oldKeyDeleted {
		removed = append(removed, Entry[K, V]{
			Key:   oldKey,
			Value: val,
		})
	}
	m.keyToValue[key] = val
	m.valueToKey[val] = key
	return removed
}

func (m *biMap[K, V]) deleteKey(key K) (V, bool) {
	val, ok := m.keyToValue[key]
	if !ok {
		return utils.Zero[V](), false
	}
	delete(m.keyToValue, key)
	delete(m.valueToKey, val)
	return val, true
}

func (m *biMap[K, V]) deleteValue(val V) (K, bool) {
	key, ok := m.valueToKey[val]
	if !ok {
		return utils.Zero[K](), false
	}
	delete(m.keyToValue, key)
	delete(m.valueToKey, val)
	return key, true
}
