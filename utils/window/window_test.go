// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package window

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
			window := New(
				Config{
					Clock:   &mockable.Clock{},
					MaxSize: testMaxSize,
					TTL:     testTTL,
				},
			)
			for _, element := range test.window {
				window.Add(element)
			}

			window.Add(test.newlyAdded)

			assert.Equal(t, len(test.window)+1, window.Length())
			oldest, ok := window.Oldest()
			assert.Equal(t, test.expectedOldest, oldest.(int))
			assert.True(t, ok)
		})
	}
}

// TestTTLAdd tests the case where an element is stale in the window
// and needs to be evicted on Add.
func TestTTLAdd(t *testing.T) {
	clock := mockable.Clock{}
	window := New(
		Config{
			Clock:   &clock,
			MaxSize: testMaxSize,
			TTL:     testTTL,
		},
	)
	epochStart := time.Unix(0, 0)
	clock.Set(epochStart)

	// Now the window looks like this:
	// [1, 2, 3]
	window.Add(1)
	window.Add(2)
	window.Add(3)

	assert.Equal(t, 3, window.Length())
	oldest, ok := window.Oldest()
	assert.Equal(t, 1, oldest.(int))
	assert.True(t, ok)
	// Now we're one second past the ttl of 10 seconds as defined in testTTL,
	// so all existing elements need to be evicted.
	clock.Set(epochStart.Add(11 * time.Second))

	// Now the window should look like this:
	// [4]
	window.Add(4)

	assert.Equal(t, 1, window.Length())
	oldest, ok = window.Oldest()
	assert.Equal(t, 4, oldest.(int))
	assert.True(t, ok)
	// Now we're one second past the ttl of 10 seconds of when [4] was added,
	// so all existing elements should be evicted.
	clock.Set(epochStart.Add(22 * time.Second))

	// Now the window should look like this:
	// []
	assert.Equal(t, 0, window.Length())

	oldest, ok = window.Oldest()
	assert.Nil(t, oldest)
	assert.False(t, ok)
}

// TestTTLReadOnly tests that stale elements are still evicted on Length
func TestTTLLength(t *testing.T) {
	clock := mockable.Clock{}
	window := New(
		Config{
			Clock:   &clock,
			MaxSize: testMaxSize,
			TTL:     testTTL,
		},
	)
	epochStart := time.Unix(0, 0)
	clock.Set(epochStart)

	// Now the window looks like this:
	// [1, 2, 3]
	window.Add(1)
	window.Add(2)
	window.Add(3)

	assert.Equal(t, 3, window.Length())

	// Now we're one second past the ttl of 10 seconds as defined in testTTL,
	// so all existing elements need to be evicted.
	clock.Set(epochStart.Add(11 * time.Second))

	// No more elements should be present in the window.
	assert.Equal(t, 0, window.Length())
}

// TestTTLReadOnly tests that stale elements are still evicted on calling Oldest
func TestTTLOldest(t *testing.T) {
	clock := mockable.Clock{}
	window := New(
		Config{
			Clock:   &clock,
			MaxSize: testMaxSize,
			TTL:     testTTL,
		},
	)
	epochStart := time.Unix(0, 0)
	clock.Set(epochStart)

	// Now the window looks like this:
	// [1, 2, 3]
	window.Add(1)
	window.Add(2)
	window.Add(3)

	oldest, ok := window.Oldest()
	assert.Equal(t, 1, oldest.(int))
	assert.True(t, ok)
	assert.Equal(t, 3, window.size)

	// Now we're one second past the ttl of 10 seconds as defined in testTTL,
	// so all existing elements need to be evicted.
	clock.Set(epochStart.Add(11 * time.Second))

	// Now there shouldn't be any elements in the window
	oldest, ok = window.Oldest()
	assert.Nil(t, oldest)
	assert.False(t, ok)
	assert.Equal(t, 0, window.size)
}

// Tests that we bound the amount of elements in the window
func TestMaxCapacity(t *testing.T) {
	window := New(
		Config{
			Clock:   &mockable.Clock{},
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

	assert.Equal(t, 3, window.Length())
	oldest, ok := window.Oldest()
	assert.Equal(t, 4, oldest.(int))
	assert.True(t, ok)
}
