// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"sync"

	"github.com/ava-labs/avalanchego/utils/set"
)

type mapFilter struct {
	lock   sync.RWMutex
	values set.Set[string]
}

func NewMap() Filter {
	return &mapFilter{}
}

func (m *mapFilter) Add(bl ...[]byte) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for _, b := range bl {
		m.values.Add(string(b))
	}
}

func (m *mapFilter) Check(b []byte) bool {
	m.lock.RLock()
	defer m.lock.RUnlock()

	return m.values.Contains(string(b))
}
