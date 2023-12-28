// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package setmap

import (
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
)

type Entry[K any, V comparable] struct {
	Key   K
	Value set.Set[V]
}

// SetMap is a map to a set where all sets are non-overlapping.
type SetMap[K, V comparable] struct {
	keyToSet   map[K]set.Set[V]
	valueToKey map[V]K
}

// New creates a new empty setmap.
func New[K, V comparable]() *SetMap[K, V] {
	return &SetMap[K, V]{
		keyToSet:   make(map[K]set.Set[V]),
		valueToKey: make(map[V]K),
	}
}

// Put the key value pair into the map. If [key] is in the map, or [val]
// overlaps with other sets, the previous entries will be removed and returned.
func (m *SetMap[K, V]) Put(key K, val set.Set[V]) []Entry[K, V] {
	removed := m.DeleteValuesOf(val)
	if removedSet, ok := m.DeleteKey(key); ok {
		removed = append(removed, Entry[K, V]{
			Key:   key,
			Value: removedSet,
		})
	}

	m.keyToSet[key] = val
	for v := range val {
		m.valueToKey[v] = key
	}
	return removed
}

// GetKey that maps to the provided value.
func (m *SetMap[K, V]) GetKey(val V) (K, bool) {
	key, ok := m.valueToKey[val]
	return key, ok
}

// GetValue that is mapped to the provided key.
func (m *SetMap[K, V]) GetValue(key K) (set.Set[V], bool) {
	val, ok := m.keyToSet[key]
	return val, ok
}

// HasKey returns true if [key] is in the map.
func (m *SetMap[K, _]) HasKey(key K) bool {
	_, ok := m.keyToSet[key]
	return ok
}

// HasValue returns true if [val] is in a set in the map.
func (m *SetMap[_, V]) HasValue(val V) bool {
	_, ok := m.valueToKey[val]
	return ok
}

// HasValuesOf returns true if [val] overlaps with any of the sets in the map.
func (m *SetMap[_, V]) HasValuesOf(val set.Set[V]) bool {
	if val.Len() < len(m.valueToKey) {
		for v := range val {
			if _, ok := m.valueToKey[v]; ok {
				return true
			}
		}
	} else {
		for v := range m.valueToKey {
			if val.Contains(v) {
				return true
			}
		}
	}
	return false
}

// DeleteKey removes [key] from the map and returns the value it mapped to.
func (m *SetMap[K, V]) DeleteKey(key K) (set.Set[V], bool) {
	val, ok := m.keyToSet[key]
	if !ok {
		return nil, false
	}

	delete(m.keyToSet, key)
	for v := range val {
		delete(m.valueToKey, v)
	}
	return val, true
}

// DeleteValue removes [val] from the map and returns the key that mapped to it
// along with the set it was contained in.
func (m *SetMap[K, V]) DeleteValue(val V) (K, set.Set[V], bool) {
	key, ok := m.valueToKey[val]
	if !ok {
		return utils.Zero[K](), nil, false
	}
	v, _ := m.DeleteKey(key)
	return key, v, true
}

// DeleteValuesOf removes all of the entries that have any overlaps with [val].
func (m *SetMap[K, V]) DeleteValuesOf(val set.Set[V]) []Entry[K, V] {
	var removed []Entry[K, V]
	for v := range val {
		if k, removedSet, ok := m.DeleteValue(v); ok {
			removed = append(removed, Entry[K, V]{
				Key:   k,
				Value: removedSet,
			})
		}
	}
	return removed
}

// Len return the number of sets in this map.
func (m *SetMap[K, V]) Len() int {
	return len(m.keyToSet)
}

// LenValues return the total number of elements across all sets in this map.
func (m *SetMap[K, V]) LenValues() int {
	return len(m.valueToKey)
}
