// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"encoding/json"
	"sync"
)

var (
	_ json.Marshaler   = (*Atomic[struct{}])(nil)
	_ json.Unmarshaler = (*Atomic[struct{}])(nil)
)

type Atomic[T any] struct {
	lock  sync.RWMutex
	value T
}

func NewAtomic[T any](value T) *Atomic[T] {
	return &Atomic[T]{
		value: value,
	}
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

func (a *Atomic[T]) Swap(value T) T {
	a.lock.Lock()
	defer a.lock.Unlock()

	old := a.value
	a.value = value

	return old
}

func (a *Atomic[T]) MarshalJSON() ([]byte, error) {
	a.lock.RLock()
	defer a.lock.RUnlock()

	return json.Marshal(a.value)
}

func (a *Atomic[T]) UnmarshalJSON(b []byte) error {
	a.lock.Lock()
	defer a.lock.Unlock()

	return json.Unmarshal(b, &a.value)
}
