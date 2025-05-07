// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

// NoopTracker is a no-op implementation of the tracker.
type NoopTracker[T comparable] struct{}

func NewNoopTracker[T comparable]() *NoopTracker[T] {
	return &NoopTracker[T]{}
}

func (NoopTracker[T]) Issue(T)                      {}
func (NoopTracker[T]) ObserveConfirmed(T)           {}
func (NoopTracker[T]) ObserveFailed(T)              {}
func (NoopTracker[T]) ObserveBlock(uint64)          {}
func (NoopTracker[T]) GetObservedConfirmed() uint64 { return 0 }
func (NoopTracker[T]) GetObservedFailed() uint64    { return 0 }
func (NoopTracker[T]) Log()                         {}
