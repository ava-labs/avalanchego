// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import "github.com/ava-labs/avalanchego/utils"

// Empty is an iterator with no elements.
type Empty[T any] struct{}

func (Empty[T]) Next() bool {
	return false
}

func (Empty[T]) Value() T {
	return utils.Zero[T]()
}

func (Empty[T]) Release() {}
