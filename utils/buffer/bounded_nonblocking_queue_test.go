// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBoundedQueue(t *testing.T) {
	require := require.New(t)

	// Case: maxSize < 1
	_, err := NewBoundedQueue[bool](0, nil)
	require.ErrorIs(err, errInvalidMaxSize)

	// Case: maxSize == 1 and nil onEvict
	b, err := NewBoundedQueue[bool](1, nil)
	require.NoError(err)

	// Put 2 elements to make sure we don't panic on evict
	b.Push(true)
	b.Push(true)
}

func TestBoundedQueue(t *testing.T) {
	require := require.New(t)

	maxSize := 3
	evicted := []int{}
	onEvict := func(elt int) {
		evicted = append(evicted, elt)
	}
	b, err := NewBoundedQueue(maxSize, onEvict)
	require.NoError(err)

	require.Zero(b.Len())

	// Fill the queue
	for i := 0; i < maxSize; i++ {
		b.Push(i)
		require.Equal(i+1, b.Len())
		got, ok := b.Peek()
		require.True(ok)
		require.Zero(got)
		got, ok = b.Index(i)
		require.True(ok)
		require.Equal(i, got)
		require.Len(b.List(), i+1)
	}
	require.Equal([]int{}, evicted)
	require.Len(b.List(), maxSize)
	// Queue is [0, 1, 2]

	// Empty the queue
	for i := 0; i < maxSize; i++ {
		got, ok := b.Pop()
		require.True(ok)
		require.Equal(i, got)
		require.Equal(maxSize-i-1, b.Len())
		require.Len(b.List(), maxSize-i-1)
	}

	// Queue is empty

	_, ok := b.Pop()
	require.False(ok)
	_, ok = b.Peek()
	require.False(ok)
	_, ok = b.Index(0)
	require.False(ok)
	require.Zero(b.Len())
	require.Empty(b.List())

	// Fill the queue again
	for i := 0; i < maxSize; i++ {
		b.Push(i)
		require.Equal(i+1, b.Len())
	}

	// Queue is [0, 1, 2]

	// Putting another element should evict the oldest.
	b.Push(maxSize)

	// Queue is [1, 2, 3]

	require.Equal(maxSize, b.Len())
	require.Len(b.List(), maxSize)
	got, ok := b.Peek()
	require.True(ok)
	require.Equal(1, got)
	got, ok = b.Index(0)
	require.True(ok)
	require.Equal(1, got)
	got, ok = b.Index(maxSize - 1)
	require.True(ok)
	require.Equal(maxSize, got)
	require.Equal([]int{0}, evicted)

	// Put 2 more elements
	b.Push(maxSize + 1)
	b.Push(maxSize + 2)

	// Queue is [3, 4, 5]

	require.Equal(maxSize, b.Len())
	require.Equal([]int{0, 1, 2}, evicted)
	got, ok = b.Peek()
	require.True(ok)
	require.Equal(3, got)
	require.Equal([]int{3, 4, 5}, b.List())

	for i := maxSize; i < 2*maxSize; i++ {
		got, ok := b.Index(i - maxSize)
		require.True(ok)
		require.Equal(i, got)
	}

	// Empty the queue
	for i := 0; i < maxSize; i++ {
		got, ok := b.Pop()
		require.True(ok)
		require.Equal(i+3, got)
		require.Equal(maxSize-i-1, b.Len())
		require.Len(b.List(), maxSize-i-1)
	}

	// Queue is empty

	require.Empty(b.List())
	require.Zero(b.Len())
	require.Equal([]int{0, 1, 2}, evicted)
	_, ok = b.Pop()
	require.False(ok)
	_, ok = b.Peek()
	require.False(ok)
	_, ok = b.Index(0)
	require.False(ok)
}
