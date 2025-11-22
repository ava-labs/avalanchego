// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodec_LazyInitialization(t *testing.T) {
	t.Parallel()

	// First call should initialize.
	c1 := Codec()
	require.NotNil(t, c1)

	// Subsequent calls should return the same instance.
	c2 := Codec()
	require.Equal(t, c1, c2)
}

func TestCodec_ThreadSafety(t *testing.T) {
	t.Parallel()

	const numGoroutines = 100
	var wg sync.WaitGroup
	results := make([]any, numGoroutines)

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = Codec()
		}(i)
	}
	wg.Wait()

	// All results should be the same instance.
	first := results[0]
	for i := 1; i < numGoroutines; i++ {
		require.Equal(t, first, results[i], "all goroutines should get the same codec instance")
	}
}
