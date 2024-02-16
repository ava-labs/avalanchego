// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cache

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
)

func TestSizedLRU(t *testing.T) {
	cache := NewSizedLRU[ids.ID, TestSizedInt](TestSizedIntSize)

	TestBasic(t, cache)
}

func TestSizedLRUEviction(t *testing.T) {
	cache := NewSizedLRU[ids.ID, TestSizedInt](2 * TestSizedIntSize)

	TestEviction(t, cache)
}
