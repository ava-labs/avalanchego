// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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
	_, ok := deque.Index(0)
	require.False(ok)

	ok = deque.PushRight(1)
	require.True(ok)
	require.Equal([]int{1}, deque.List())
	got, ok := deque.Index(0)
	require.True(ok)
	require.Equal(1, got)

	ok = deque.PushRight(2)
	require.True(ok)
	require.Equal([]int{1, 2}, deque.List())
	got, ok = deque.Index(0)
	require.True(ok)
	require.Equal(1, got)
	got, ok = deque.Index(1)
	require.True(ok)
	require.Equal(2, got)
	_, ok = deque.Index(2)
	require.False(ok)

	ch, ok := deque.PopLeft()
	require.True(ok)
	require.Equal(1, ch)
	require.Equal([]int{2}, deque.List())
	got, ok = deque.Index(0)
	require.True(ok)
	require.Equal(2, got)
}

func TestUnboundedBlockingDequePop(t *testing.T) {
	require := require.New(t)

	deque := NewUnboundedBlockingDeque[int](2)
	require.Empty(deque.List())

	ok := deque.PushRight(1)
	require.True(ok)
	require.Equal([]int{1}, deque.List())
	got, ok := deque.Index(0)
	require.True(ok)
	require.Equal(1, got)

	ch, ok := deque.PopLeft()
	require.True(ok)
	require.Equal(1, ch)
	require.Empty(deque.List())

	var (
		gotOk bool
		gotCh int
	)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		gotCh, gotOk = deque.PopLeft()
		wg.Done()
	}()

	ok = deque.PushRight(2)
	require.True(ok)
	wg.Wait()

	require.True(gotOk)
	require.Equal(2, gotCh)

	require.Empty(deque.List())
	_, ok = deque.Index(0)
	require.False(ok)
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
