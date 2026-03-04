// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import "github.com/ava-labs/avalanchego/utils"

var _ Iterator[any] = Empty[any]{}

// Empty is an iterator with no elements.
type Empty[T any] struct{}

func (Empty[_]) Next() bool {
	return false
}

func (Empty[T]) Value() T {
	return utils.Zero[T]()
}

func (Empty[_]) Release() {}
