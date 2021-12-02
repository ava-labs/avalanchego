// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"sync"
)

type mapFilter struct {
	lock   sync.RWMutex
	values map[string]struct{}
}

func NewMap() Filter {
	return &mapFilter{
		values: make(map[string]struct{}),
	}
}

func (m *mapFilter) Add(bl ...[]byte) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, b := range bl {
		m.values[string(b)] = struct{}{}
	}
}

func (m *mapFilter) Check(b []byte) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()

	_, exists := m.values[string(b)]
	return exists
}
