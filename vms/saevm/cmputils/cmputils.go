// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build !prod && !nocmpopts

// Package cmputils provides [cmp] options and utilities for their creation.
package cmputils

import (
	"reflect"
	"testing"

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

// IfInField returns a filtered equivalent of `opt` such that it is only
// evaluated if the [cmp.Path] includes a `T` along with a field of the
// specified name and type as the next [cmp.PathStep].
//
// To protect against changes in field names, it always confirms that said field
// exists and has the expected type. Although inferior to compile-time checks,
// this approach is cleaner than introducing marker types to use [IfIn].
func IfInField[Struct, Field any](tb testing.TB, fieldName string, opt cmp.Option) cmp.Option {
	tb.Helper()

	typ := reflect.TypeFor[Struct]()
	if f, ok := typ.FieldByName(fieldName); !ok || f.Type != reflect.TypeFor[Field]() {
		var (
			s Struct
			f Field
		)
		tb.Fatalf("Type %T does not contain field %q of type %T to filter cmp.Option", s, fieldName, f)
	}

	step := "." + fieldName
	return cmp.FilterPath(
		func(p cmp.Path) bool {
			inType := false
			for _, s := range p {
				if inType && s.String() == step {
					return true
				}
				inType = s.Type() == typ
			}
			return false
		},
		opt,
	)
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
