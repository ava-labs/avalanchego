// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"sync"
)

type Atomic[T any] struct {
	lock  sync.RWMutex
	value T
}

func (a *Atomic[T]) Get() T {
	a.lock.RLock()
	defer a.lock.RUnlock()

	return a.value
}

func (a *Atomic[T]) Set(value T) {
	a.lock.Lock()
	defer a.lock.Unlock()

	a.value = value
}
