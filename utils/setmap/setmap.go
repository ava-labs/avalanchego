// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package setmap

import (
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
)

type Entry[K any, V comparable] struct {
	Key K
	Set set.Set[V]
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

// Put the new entry into the map. Removes and returns:
// * The existing entry for [key].
// * Existing entries where the set overlaps with the [set].
func (m *SetMap[K, V]) Put(key K, set set.Set[V]) []Entry[K, V] {
	removed := m.DeleteOverlapping(set)
	if removedSet, ok := m.DeleteKey(key); ok {
		removed = append(removed, Entry[K, V]{
			Key: key,
			Set: removedSet,
		})
	}

	m.keyToSet[key] = set
	for val := range set {
		m.valueToKey[val] = key
	}
	return removed
}

// GetKey that maps to the provided value.
func (m *SetMap[K, V]) GetKey(val V) (K, bool) {
	key, ok := m.valueToKey[val]
	return key, ok
}

// GetSet that is mapped to by the provided key.
func (m *SetMap[K, V]) GetSet(key K) (set.Set[V], bool) {
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

// HasOverlap returns true if [set] overlaps with any of the sets in the map.
func (m *SetMap[_, V]) HasOverlap(set set.Set[V]) bool {
	if set.Len() < len(m.valueToKey) {
		for val := range set {
			if _, ok := m.valueToKey[val]; ok {
				return true
			}
		}
	} else {
		for val := range m.valueToKey {
			if set.Contains(val) {
				return true
			}
		}
	}
	return false
}

// DeleteKey removes [key] from the map and returns the set it mapped to.
func (m *SetMap[K, V]) DeleteKey(key K) (set.Set[V], bool) {
	set, ok := m.keyToSet[key]
	if !ok {
		return nil, false
	}

	delete(m.keyToSet, key)
	for val := range set {
		delete(m.valueToKey, val)
	}
	return set, true
}

// DeleteValue removes and returns the entry that contained [val].
func (m *SetMap[K, V]) DeleteValue(val V) (K, set.Set[V], bool) {
	key, ok := m.valueToKey[val]
	if !ok {
		return utils.Zero[K](), nil, false
	}
	set, _ := m.DeleteKey(key)
	return key, set, true
}

// DeleteOverlapping removes and returns all the entries where the set overlaps
// with [set].
func (m *SetMap[K, V]) DeleteOverlapping(set set.Set[V]) []Entry[K, V] {
	var removed []Entry[K, V]
	for val := range set {
		if k, removedSet, ok := m.DeleteValue(val); ok {
			removed = append(removed, Entry[K, V]{
				Key: k,
				Set: removedSet,
			})
		}
	}
	return removed
}

// Len return the number of sets in the map.
func (m *SetMap[K, V]) Len() int {
	return len(m.keyToSet)
}

// LenValues return the total number of values across all sets in the map.
func (m *SetMap[K, V]) LenValues() int {
	return len(m.valueToKey)
}
