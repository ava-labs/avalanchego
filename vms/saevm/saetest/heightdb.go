// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetest

import (
	"slices"
	"sync"

	"github.com/ava-labs/avalanchego/database"

	"github.com/ava-labs/avalanchego/vms/saevm/types"
)

// A ClonableHeightIndex extends [database.HeightIndex] with the ability to
// clone itself.
type ClonableHeightIndex interface {
	database.HeightIndex
	Clone() ClonableHeightIndex
}

// NewHeightIndexDB returns an in-memory [database.HeightIndex]; its additional
// `Clone()` method can be called before or after closing, and the clone will
// not be closed in either circumstance. Only heights for which `Sync()` has
// returned without error will be cloned.
func NewHeightIndexDB() ClonableHeightIndex {
	return &hIndex{
		pending: make(map[uint64]bool),
		data:    make(map[uint64][]byte),
	}
}

// NewExecutionResultsDB wraps and returns a [NewHeightIndexDB].
func NewExecutionResultsDB() types.ExecutionResults {
	return types.ExecutionResults{HeightIndex: NewHeightIndexDB()}
}

type hIndex struct {
	mu      sync.RWMutex
	pending map[uint64]bool
	data    map[uint64][]byte
	closed  bool
}

func readHIndex[T any](h *hIndex, fn func() (T, error)) (T, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		var zero T
		return zero, database.ErrClosed
	}
	return fn()
}

func (h *hIndex) write(fn func() error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return database.ErrClosed
	}
	return fn()
}

func (h *hIndex) Put(n uint64, b []byte) error {
	return h.write(func() error {
		h.pending[n] = true
		h.data[n] = slices.Clone(b)
		return nil
	})
}

func (h *hIndex) Get(n uint64) ([]byte, error) {
	return readHIndex(h, func() ([]byte, error) {
		b, ok := h.data[n]
		if !ok {
			return nil, database.ErrNotFound
		}
		return slices.Clone(b), nil
	})
}

func (h *hIndex) Clone() ClonableHeightIndex {
	h.mu.RLock()
	defer h.mu.RUnlock()

	cp := NewHeightIndexDB().(*hIndex) //nolint:forcetypeassert // Internal invariant
	for k, v := range h.data {
		if !h.pending[k] {
			cp.data[k] = v
		}
	}
	return cp
}

func (h *hIndex) Has(n uint64) (bool, error) {
	return readHIndex(h, func() (bool, error) {
		_, ok := h.data[n]
		return ok, nil
	})
}

func (h *hIndex) Sync(from, to uint64) error {
	return h.write(func() error {
		for i := from; i <= to; i++ {
			delete(h.pending, i)
		}
		return nil
	})
}

func (h *hIndex) Close() error {
	return h.write(func() error {
		h.closed = true
		return nil
	})
}
