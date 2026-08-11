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
func (c *Closers) Push(cs ...io.Closer) {
	*c = append(*c, cs...)
}

// Close closes every [io.Closer], in reverse order.
func (c Closers) Close() error {
	return errors.Join(c.close()...)
}

func (c Closers) close() []error {
	errs := make([]error, 0, len(c))
	for _, c := range slices.Backward(c) {
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
func (c *Closers) CloseIfPointsToNonNil(retErr *error) {
	// The receiver MUST be a pointer because this method is typically deferred
	// before any call to [closers.push], and a `defer` statement evaluates its
	// receiver when the statement executes, not when the call runs.
	if *retErr != nil {
		*retErr = errors.Join(slices.Concat(
			[]error{*retErr},
			c.close(),
		)...)
	}
}

// A CloserFunc converts a function into an [io.Closer].
type CloserFunc func() error

// Close returns `f()`.
func (f CloserFunc) Close() error { return f() }

// ClsoerFuncT returns an [io.Closer] for which the `Close()` method returns
// `fn(x)`.
func CloserFuncT[T any](fn func(T) error, x T) io.Closer {
	return CloserFunc(func() error { return fn(x) })
}
