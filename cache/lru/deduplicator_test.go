// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lru

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

type evictable[K comparable] struct {
	id      K
	evicted int
}

func (e *evictable[K]) Key() K {
	return e.id
}

func (e *evictable[_]) Evict() {
	e.evicted++
}

func TestDeduplicator(t *testing.T) {
	require := require.New(t)

	cache := NewDeduplicator[ids.ID, *evictable[ids.ID]](1)

	expectedValue1 := &evictable[ids.ID]{id: ids.ID{1}}
	require.Equal(expectedValue1, cache.Deduplicate(expectedValue1))
	require.Zero(expectedValue1.evicted)
	require.Equal(expectedValue1, cache.Deduplicate(expectedValue1))
	require.Zero(expectedValue1.evicted)

	expectedValue2 := &evictable[ids.ID]{id: ids.ID{2}}
	returnedValue := cache.Deduplicate(expectedValue2)
	require.Equal(expectedValue2, returnedValue)
	require.Equal(1, expectedValue1.evicted)
	require.Zero(expectedValue2.evicted)
}
