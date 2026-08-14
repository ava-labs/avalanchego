// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package unwind provides a mechanism for dismantling objects that are
// constructed in stages should there be a failure partway through.
package unwind

import (
	"errors"
	"io"
	"slices"
	"sync"
)

// Closers are closed in the opposite order to which they're pushed.
type Closers []io.Closer

// Push is a convenience wrapper for:
//
//	*c = append(*c, cs...)
func (cs *Closers) Push(xx ...io.Closer) {
	*cs = append(*cs, xx...)
}

// Close closes every [io.Closer], in reverse order.
func (cs Closers) Close() error {
	return errors.Join(cs.close()...)
}

func (cs Closers) close() []error {
	return closeAll(cs, io.Closer.Close)
}

// closeAll closes every closer, in reverse order.
func closeAll[C any](cs []C, closer func(C) error) []error {
	errs := make([]error, 0, len(cs))
	for _, c := range slices.Backward(cs) {
		// Reported in the same order as executed to allow tests to assert the
		// reversal.
		errs = append(errs, closer(c))
	}
	return errs
}

// joinInto joins the error pointed to by `retErr`, if non-nil, with all errors
// returned by `close`.
func joinInto(retErr *error, closeAll func() []error) {
	if *retErr != nil {
		*retErr = errors.Join(slices.Concat(
			[]error{*retErr},
			closeAll(),
		)...)
	}
}

// CloseIfPointsToNonNil closes all closers, in reverse order, i.f.f. `retErr`
// points to a non-nil error, joining any resulting errors into it. It is
// expected to be defer-called in a function, with a pointer to said function's
// named return argument, as demonstrated in the example.
func (cs *Closers) CloseIfPointsToNonNil(retErr *error) {
	// The receiver MUST be a pointer because this method is typically deferred
	// before any call to [Closers.Push], and a `defer` statement evaluates its
	// receiver when the statement executes, not when the call runs.
	joinInto(retErr, cs.close)
}

// A CloserFunc converts a function into an [io.Closer].
type CloserFunc func() error

// Close returns `f()`.
func (f CloserFunc) Close() error { return f() }

// CloserFuncWith returns an [io.Closer] for which the `Close()` method returns
// `fn(x)`.
func CloserFuncWith[T any](fn func(T) error, x T) io.Closer {
	return CloserFunc(func() error { return fn(x) })
}

// CloserOf is like an [io.Closer] but takes an argument of type T.
type CloserOf[T any] interface {
	Close(T) error
}

// CloserOfFunc converts a function into a [CloserOf].
type CloserOfFunc[T any] func(T) error

func (f CloserOfFunc[T]) Close(arg T) error { return f(arg) }

// NoArgCloserOf is a [CloserOf] that ignores its argument.
type NoArgCloserOf[T any] func() error

func (f NoArgCloserOf[T]) Close(T) error { return f() }

// ClosersOf is a slice of [CloserOf]s that are closed in the opposite order to
// which they're pushed. All methods are concurrent safe.
type ClosersOf[T any] struct {
	mu      sync.Mutex
	closers []CloserOf[T]
}

// NewClosersOf returns a new [ClosersOf] containing the provided closers.
func NewClosersOf[T any](closers ...CloserOf[T]) *ClosersOf[T] {
	return &ClosersOf[T]{closers: closers}
}

// Push is a convenience wrapper for:
//
//	*c = append(*c, cs...)
func (cs *ClosersOf[T]) Push(xx ...CloserOf[T]) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.closers = append(cs.closers, xx...)
}

// Close closes every [CloserOf], in reverse order.
//
// Any subsequent call to either Close or CloseIfPointsToNonNil will have no
// effect until more closers are pushed.
func (cs *ClosersOf[T]) Close(arg T) error {
	return errors.Join(cs.close(arg)...)
}

func (cs *ClosersOf[T]) close(arg T) []error {
	cs.mu.Lock()
	defer func() {
		cs.closers = nil
		cs.mu.Unlock()
	}()
	return closeAll(cs.closers, func(c CloserOf[T]) error { return c.Close(arg) })
}

// CloseIfPointsToNonNil closes all closers, in reverse order, i.f.f. `retErr`
// points to a non-nil error, joining any resulting errors into it. It is
// expected to be defer-called in a function, with a pointer to said function's
// named return argument, as demonstrated in the example.
//
// Any subsequent call to either Close or CloseIfPointsToNonNil will have no
// effect until more closers are pushed.
func (cs *ClosersOf[T]) CloseIfPointsToNonNil(arg T, retErr *error) {
	joinInto(retErr, func() []error { return cs.close(arg) })
}
