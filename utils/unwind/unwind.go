// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package unwind provides a mechanism for dismantling objects that are
// constructed in stages should there be a failure partway through.
package unwind

import (
	"errors"
	"io"
	"slices"
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
	errs := make([]error, 0, len(cs))
	for _, c := range slices.Backward(cs) {
		// Reported in the same order as executed to allow tests to assert the
		// reversal.
		errs = append(errs, c.Close())
	}
	return errs
}

// CloseIfPointsToNonNil closes all closers, in reverse order, i.f.f. `retErr`
// points to a non-nil error, joining any resulting errors into it. It is
// expected to be defer-called in a function, with a pointer to said function's
// named return argument, as demonstrated in the example.
func (cs *Closers) CloseIfPointsToNonNil(retErr *error) {
	// The receiver MUST be a pointer because this method is typically deferred
	// before any call to [Closers.Push], and a `defer` statement evaluates its
	// receiver when the statement executes, not when the call runs.
	if *retErr != nil {
		*retErr = errors.Join(slices.Concat(
			[]error{*retErr},
			cs.close(),
		)...)
	}
}

// A CloserFunc converts a function into an [io.Closer].
type CloserFunc func() error

// Close returns `f()`.
func (f CloserFunc) Close() error { return f() }

// CloserFuncT returns an [io.Closer] for which the `Close()` method returns
// `fn(x)`.
func CloserFuncT[T any](fn func(T) error, x T) io.Closer {
	return CloserFunc(func() error { return fn(x) })
}
