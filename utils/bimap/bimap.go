// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bimap

import "github.com/ava-labs/avalanchego/utils"

type Entry[K, V any] struct {
	Key   K
	Value V
}

// BiMap is a bi-directional map.
type BiMap[K, V comparable] struct {
	keyToValue map[K]V
	valueToKey map[V]K
}

// New creates a new empty bimap.
func New[K, V comparable]() *BiMap[K, V] {
	return &BiMap[K, V]{
		keyToValue: make(map[K]V),
		valueToKey: make(map[V]K),
	}
}

// Put the key value pair into the map. If either [key] or [val] was previously
// in the map, the previous entries will be removed and returned.
//
// Note: Unlike normal maps, it's possible that Put removes 0, 1, or 2 existing
// entries to ensure that mappings are one-to-one.
func (m *BiMap[K, V]) Put(key K, val V) []Entry[K, V] {
	var removed []Entry[K, V]
	oldVal, oldValDeleted := m.DeleteKey(key)
	if oldValDeleted {
		removed = append(removed, Entry[K, V]{
			Key:   key,
			Value: oldVal,
		})
	}
	oldKey, oldKeyDeleted := m.DeleteValue(val)
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

// GetKey that maps to the provided value.
func (m *BiMap[K, V]) GetKey(val V) (K, bool) {
	key, ok := m.valueToKey[val]
	return key, ok
}

// GetValue that is mapped to the provided key.
func (m *BiMap[K, V]) GetValue(key K) (V, bool) {
	val, ok := m.keyToValue[key]
	return val, ok
}

// HasKey returns true if [key] is in the map.
func (m *BiMap[K, _]) HasKey(key K) bool {
	_, ok := m.keyToValue[key]
	return ok
}

// HasValue returns true if [val] is in the map.
func (m *BiMap[_, V]) HasValue(val V) bool {
	_, ok := m.valueToKey[val]
	return ok
}

// DeleteKey removes [key] from the map and returns the value it mapped to.
func (m *BiMap[K, V]) DeleteKey(key K) (V, bool) {
	val, ok := m.keyToValue[key]
	if !ok {
		return utils.Zero[V](), false
	}
	delete(m.keyToValue, key)
	delete(m.valueToKey, val)
	return val, true
}

// DeleteValue removes [val] from the map and returns the key that mapped to it.
func (m *BiMap[K, V]) DeleteValue(val V) (K, bool) {
	key, ok := m.valueToKey[val]
	if !ok {
		return utils.Zero[K](), false
	}
	delete(m.keyToValue, key)
	delete(m.valueToKey, val)
	return key, true
}

// Len return the number of entries in this map.
func (m *BiMap[K, V]) Len() int {
	return len(m.keyToValue)
}
