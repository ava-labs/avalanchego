// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !prod && !nocmpopts

// Package cmputils provides [cmp] options and utilities for their creation.
package cmputils

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
)

// IfIn returns a filtered equivalent of `opt` such that it is only evaluated if
// the [cmp.Path] includes at least one `T`. This is typically used for struct
// fields (and sub-fields).
func IfIn[T any](opt cmp.Option) cmp.Option {
	return cmp.FilterPath(pathIncludes[T], opt)
}

func pathIncludes[T any](p cmp.Path) bool {
	t := reflect.TypeFor[T]()
	for _, step := range p {
		if step.Type() == t {
			return true
		}
	}
	return false
}

// WithNilCheck returns a function that returns:
//
//	   true if both a and b are nil
//	  false if exactly one of a or b is nil
//	fn(a,b) if neither a nor b are nil
func WithNilCheck[T any](fn func(*T, *T) bool) func(*T, *T) bool {
	return func(a, b *T) bool {
		switch an, bn := a == nil, b == nil; {
		case an && bn:
			return true
		case an || bn:
			return false
		}
		return fn(a, b)
	}
}

// ComparerWithNilCheck is a convenience wrapper, returning a [cmp.Comparer]
// after wrapping `fn` in [WithNilCheck].
func ComparerWithNilCheck[T any](fn func(*T, *T) bool) cmp.Option {
	return cmp.Comparer(WithNilCheck(fn))
}
