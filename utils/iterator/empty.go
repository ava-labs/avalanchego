// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import "github.com/ava-labs/avalanchego/utils"

// Empty returns an iterator with no elements.
func Empty[T any]() Iterator[T] {
	return &empty[T]{}
}

type empty[T any] struct{}

func (empty[T]) Next() bool {
	return false
}

func (empty[T]) Value() T {
	return utils.Zero[T]()
}

func (empty[T]) Release() {}
