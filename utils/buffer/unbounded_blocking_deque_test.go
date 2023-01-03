// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package buffer

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUnboundedBlockingDequePush(t *testing.T) {
	require := require.New(t)

	deque := NewUnboundedBlockingDeque[int](2)
	require.Empty(deque.List())

	ok := deque.PushRight(1)
	require.True(ok)
	require.Equal([]int{1}, deque.List())

	ok = deque.PushRight(2)
	require.True(ok)
	require.Equal([]int{1, 2}, deque.List())

	ch, ok := deque.PopLeft()
	require.True(ok)
	require.Equal(1, ch)
	require.Equal([]int{2}, deque.List())
}

func TestUnboundedBlockingDequePop(t *testing.T) {
	require := require.New(t)

	deque := NewUnboundedBlockingDeque[int](2)
	require.Empty(deque.List())

	ok := deque.PushRight(1)
	require.True(ok)
	require.Equal([]int{1}, deque.List())

	ch, ok := deque.PopLeft()
	require.True(ok)
	require.Equal(1, ch)
	require.Empty(deque.List())

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		ch, ok := deque.PopLeft()
		require.True(ok)
		require.Equal(2, ch)
		wg.Done()
	}()

	ok = deque.PushRight(2)
	require.True(ok)
	wg.Wait()
	require.Empty(deque.List())
}

func TestUnboundedBlockingDequeClose(t *testing.T) {
	require := require.New(t)

	deque := NewUnboundedBlockingDeque[int](2)

	ok := deque.PushLeft(1)
	require.True(ok)

	deque.Close()

	_, ok = deque.PopRight()
	require.False(ok)

	ok = deque.PushLeft(1)
	require.False(ok)
}
