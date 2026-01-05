// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package window

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/timer/mockable"
)

const (
	testTTL     = 10 * time.Second
	testMaxSize = 10
)

// TestAdd tests that elements are populated as expected, ignoring
// any eviction.
func TestAdd(t *testing.T) {
	tests := []struct {
		name           string
		window         []int
		newlyAdded     int
		expectedOldest int
	}{
		{
			name:           "empty",
			window:         []int{},
			newlyAdded:     1,
			expectedOldest: 1,
		},
		{
			name:           "populated",
			window:         []int{1, 2, 3, 4},
			newlyAdded:     5,
			expectedOldest: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			clock := &mockable.Clock{}
			clock.Set(time.Now())

			window := New[int](
				Config{
					Clock:   clock,
					MaxSize: testMaxSize,
					TTL:     testTTL,
				},
			)
			for _, element := range test.window {
				window.Add(element)
			}

			window.Add(test.newlyAdded)

			require.Equal(len(test.window)+1, window.Length())
			oldest, ok := window.Oldest()
			require.True(ok)
			require.Equal(test.expectedOldest, oldest)
		})
	}
}

// TestTTLAdd tests the case where an element is stale in the window
// and needs to be evicted on Add.
func TestTTLAdd(t *testing.T) {
	require := require.New(t)

	clock := &mockable.Clock{}
	start := time.Now()
	clock.Set(start)

	window := New[int](
		Config{
			Clock:   clock,
			MaxSize: testMaxSize,
			TTL:     testTTL,
		},
	)

	// Now the window looks like this:
	// [1, 2, 3]
	window.Add(1)
	window.Add(2)
	window.Add(3)

	require.Equal(3, window.Length())
	oldest, ok := window.Oldest()
	require.True(ok)
	require.Equal(1, oldest)
	// Now we're one second past the ttl of 10 seconds as defined in testTTL,
	// so all existing elements need to be evicted.
	clock.Set(start.Add(testTTL + time.Second))

	// Now the window should look like this:
	// [4]
	window.Add(4)

	require.Equal(1, window.Length())
	oldest, ok = window.Oldest()
	require.True(ok)
	require.Equal(4, oldest)
	// Now we're one second before the ttl of 10 seconds of when [4] was added,
	// no element should be evicted
	// [4, 5]
	clock.Set(start.Add(2 * testTTL))
	window.Add(5)
	require.Equal(2, window.Length())
	oldest, ok = window.Oldest()
	require.True(ok)
	require.Equal(4, oldest)

	// Now the window is still containing 4:
	// [4, 5]
	// we only evict on Add method because the window is calculated in the last element added
	require.Equal(2, window.Length())

	oldest, ok = window.Oldest()
	require.True(ok)
	require.Equal(4, oldest)
}

// TestTTLLength tests that elements are evicted on Length
func TestTTLLength(t *testing.T) {
	require := require.New(t)

	clock := &mockable.Clock{}
	start := time.Now()
	clock.Set(start)

	window := New[int](
		Config{
			Clock:   clock,
			MaxSize: testMaxSize,
			TTL:     testTTL,
		},
	)

	// Now the window looks like this:
	// [1, 2, 3]
	window.Add(1)
	window.Add(2)
	window.Add(3)

	require.Equal(3, window.Length())

	// Now we're one second past the ttl of 10 seconds as defined in testTTL,
	// so all existing elements need to be evicted.
	clock.Set(start.Add(testTTL + time.Second))

	// No more elements should be present in the window.
	require.Equal(0, window.Length())
}

// TestTTLOldest tests that stale elements are evicted on calling Oldest
func TestTTLOldest(t *testing.T) {
	require := require.New(t)

	clock := &mockable.Clock{}
	start := time.Now()
	clock.Set(start)

	windowIntf := New[int](
		Config{
			Clock:   clock,
			MaxSize: testMaxSize,
			TTL:     testTTL,
		},
	)
	require.IsType(&window[int]{}, windowIntf)
	window := windowIntf.(*window[int])

	// Now the window looks like this:
	// [1, 2, 3]
	window.Add(1)
	window.Add(2)
	window.Add(3)

	oldest, ok := window.Oldest()
	require.True(ok)
	require.Equal(1, oldest)
	require.Equal(3, window.elements.Len())

	// Now we're one second before the ttl of 10 seconds as defined in testTTL,
	// so all existing elements should still exist.
	// Add 4 to the window to make it:
	// [1, 2, 3, 4]
	clock.Set(start.Add(testTTL - time.Second))
	window.Add(4)

	oldest, ok = window.Oldest()
	require.True(ok)
	require.Equal(1, oldest)
	require.Equal(4, window.elements.Len())

	// Now we're one second past the ttl of the initial 3 elements
	// call to oldest should now evict 1,2,3 and return 4.
	clock.Set(start.Add(testTTL + time.Second))
	oldest, ok = window.Oldest()
	require.True(ok)
	require.Equal(4, oldest)
	require.Equal(1, window.elements.Len())
}

// Tests that we bound the amount of elements in the window
func TestMaxCapacity(t *testing.T) {
	require := require.New(t)

	clock := &mockable.Clock{}
	clock.Set(time.Now())

	window := New[int](
		Config{
			Clock:   clock,
			MaxSize: 3,
			TTL:     testTTL,
		},
	)

	// Now the window looks like this:
	// [1, 2, 3]
	window.Add(1)
	window.Add(2)
	window.Add(3)

	// We should evict 1 and replace it with 4.
	// Now the window should look like this:
	// [2, 3, 4]
	window.Add(4)
	// We should evict 2 and replace it with 5.
	// Now the window should look like this:
	// [3, 4, 5]
	window.Add(5)
	// We should evict 3 and replace it with 6.
	// Now the window should look like this:
	// [4, 5, 6]
	window.Add(6)

	require.Equal(3, window.Length())
	oldest, ok := window.Oldest()
	require.True(ok)
	require.Equal(4, oldest)
}

// Tests that we do not evict past the minimum window size
func TestMinCapacity(t *testing.T) {
	require := require.New(t)

	clock := &mockable.Clock{}
	start := time.Now()
	clock.Set(start)

	window := New[int](
		Config{
			Clock:   clock,
			MaxSize: 3,
			MinSize: 2,
			TTL:     testTTL,
		},
	)

	// Now the window looks like this:
	// [1, 2, 3]
	window.Add(1)
	window.Add(2)
	window.Add(3)

	clock.Set(start.Add(testTTL + time.Second))

	// All of [1, 2, 3] are past the ttl now, but we don't evict 3 because of
	// the minimum length.
	// Now the window should look like this:
	// [3, 4]
	window.Add(4)

	require.Equal(2, window.Length())
	oldest, ok := window.Oldest()
	require.True(ok)
	require.Equal(3, oldest)
}
